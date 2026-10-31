# Colligo — Technical Specification

Declarative document. It describes **what exists**, without narrative. For the why, see `DESIGN.md`. For the defences against silent error, `RELIABILITY.md`. For leakage of restricted data, `CONFIDENTIALITY.md`. Implement from here.

Module: `github.com/colligo-go/colligo`
Go: the version pinned in `go.mod`.
SQLite driver: `modernc.org/sqlite`.
BLAKE3: `lukechampine.com/blake3`.

---

## 1. Package structure

```
colligo/
├── colligo.go        public entry point: Open(), Config, Export, SourceFor
├── model/            exported core types
├── secret/           Secret, Name, Endpoint
├── store/            SQLite implementation of Store; embedded migrations
├── source/           one sub-package per source, with no access to disk or database
│   ├── ena/
│   ├── gencode/
│   ├── ensembl/
│   ├── geo/
│   ├── cbioportal/
│   ├── pdb/
│   └── dfam/
├── transport/        HTTP, FTP, DRS; minimal surface
├── profile/          pluggable profiles
├── facts/            single streaming read: BLAKE3 tree with Bao outboard, MD5, SHA-256 and format facts
├── downloader/       worker pools, retry, rate limit, resume
├── adopt/            registration of a pre-existing file under custody
├── verifier/         funnel: stat gate, facts, assertions, comparison with a source declaration, scrub
├── format/           structural validators that also extract facts
├── samplesheet/      reads nf-core samplesheets (rnaseq and generic) and resolves their paths; never opens a data file
├── identity/         identification by content: seqcol, GTF header, hash against official checksums
├── compat/           comparison of coordinate systems by Sequence Collections
├── upstream/         re-query of metadata at the source and comparison with the stored one
├── validator/        two-phase metadata validation
├── manifest/         export and attestation of a portable manifest
├── exporter/         TSV/JSON/CSV with custody
├── checksum/         known vectors of BLAKE3, MD5 and SHA-256, run before any hash
├── logf/             typed log fields; no `string` field for path, URL or credential
├── retry/            policies: exponential, linear, custom
├── sourcetest/       adapter conformance suite (public API)
├── storetest/        Store conformance suite
├── mutants/          (planned, Slice 7) generator of silent failures for test and benchmark
├── diagnoses/        versioned catalogue of diagnoses: observation, probable cause, action
├── spec/             TLA+ models of resume, rename and the state machine
├── tools/            independent certificate verifier and the checksum table generator (Python)
├── cmd/colligo/      CLI: light door and heavy door (§16)
└── internal/         unexported helpers: noleak, fsaccess, exttool, corpus, buildinfo, archtest
```

The tree is the destination. The packages enter in the slices of `ROADMAP.md`: today `secret`, `logf`, `model`, `checksum`, `facts`, `format`, `identity` (the seqcol digest of a FASTA only), `samplesheet`, `verifier`, `manifest` (the certificate only), `diagnoses`, `transport` (metadata only), `source/ena` (metadata only), `tools` and `cmd/colligo` exist. Without `mutants/`, the coverage of failure modes comes from the versioned synthetic corpus and from the tests of each defence.

**Import graph rule, verified in test:** `model`, `store`, `downloader`, `verifier`, `facts`, `format`, `samplesheet` and `checksum` do not import `source/*`. Source registration is explicit via an option; there is no `init()` with side effects.

**Two-doors rule, verified in test:** the path of `colligo check`, `validate`, `identify` and `compat` does not import `store`. `facts`, `format`, `samplesheet`, `identity`, `compat` and the metadata part of `source/*` do not depend on a database.

---

## 2. PRAGMAs

Opening per `CONTRACT.md` §2, identical in Scribo. Apply on every connection and **read back** to confirm they took effect.

```
foreign_keys = ON
journal_mode = <resolved by the filesystem>
synchronous  = NORMAL
busy_timeout = 5000
```

Journal mode: `statfs` on the database directory; local filesystem → `WAL`; network filesystem (`nfs`, `nfs4`, `cifs`, `smb2`, `lustre`, `gpfs`, `fuse.*`, `afs`, `9p`) → `DELETE`, with a warning. WAL requires shared memory between processes on the same host; over NFS it is silent corruption. An explicit override is recorded in `tool_runs`. Write transactions open with `_txlock=immediate`.

`*sql.DB` is a pool; a PRAGMA via `db.Exec` configures only one connection. The PRAGMAs go in the DSN, which the driver applies to every new connection:

```
file:study.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_txlock=immediate
```

The read-back runs in the new-connection hook, not once at opening. A divergence between the requested value and the value read → `fatal` error. Tables use `STRICT` (SQLite 3.37+). `mmap_size` and `cache_size` are tunable.

---

## 3. SQLite schema

```sql
CREATE TABLE study_meta (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE sources (
    id             TEXT PRIMARY KEY,
    classification TEXT NOT NULL CHECK (classification IN ('public','restricted')),
    declared_at    TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS tool_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    contract_version INTEGER NOT NULL,
    tool             TEXT NOT NULL CHECK (tool IN ('colligo','scribo')),
    tool_version     TEXT NOT NULL,
    git_sha          TEXT NOT NULL,
    go_version       TEXT NOT NULL,
    host             TEXT,
    step             TEXT,
    config_hash      TEXT,
    argv_shape       TEXT NOT NULL,
    started_at       TEXT NOT NULL,
    ended_at         TEXT,
    exit_code        INTEGER
) STRICT;

CREATE TABLE experiments (
    id                  TEXT PRIMARY KEY,
    source              TEXT NOT NULL REFERENCES sources(id),
    type                TEXT NOT NULL,
    condition           TEXT,
    organism            TEXT,
    declared_at         TEXT NOT NULL,
    fetched_at          TEXT,
    expected_file_count INTEGER,
    expected_set_hash   TEXT,
    status              TEXT NOT NULL DEFAULT 'pending'
) STRICT;

CREATE TABLE samples (
    id                 TEXT PRIMARY KEY,
    experiment_id      TEXT NOT NULL REFERENCES experiments(id),
    accession          TEXT NOT NULL,
    role               TEXT NOT NULL,
    replicate          TEXT,
    paired             INTEGER,
    confidence         TEXT NOT NULL,
    source_status      TEXT,
    source_checked_at  TEXT,
    UNIQUE(experiment_id, accession)
) STRICT;

CREATE TABLE files (
    id                 TEXT PRIMARY KEY,
    sample_id          TEXT NOT NULL REFERENCES samples(id),
    origin             TEXT NOT NULL CHECK (origin IN ('fetched','adopted')),
    url                TEXT,
    url_query_stripped INTEGER NOT NULL DEFAULT 0,
    path               TEXT,
    path_part          TEXT,
    format             TEXT,
    format_confidence  TEXT,
    size_expected      INTEGER,
    md5_expected       TEXT,
    sha256_expected    TEXT,
    reads_expected     INTEGER,
    bases_expected     INTEGER,
    blake3_root        TEXT,
    outboard_path      TEXT,
    etag               TEXT,
    last_modified      TEXT,
    pair_id            TEXT,
    supersedes         TEXT REFERENCES files(id),
    status             TEXT NOT NULL DEFAULT 'pending',
    attempts           INTEGER NOT NULL DEFAULT 0,
    last_verified_at   TEXT,
    verified_size      INTEGER,
    verified_mtime     TEXT,
    verified_inode     INTEGER,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    UNIQUE(sample_id, url, supersedes)
) STRICT;

CREATE TABLE file_groups (
    file_id     TEXT NOT NULL REFERENCES files(id),
    idx         INTEGER NOT NULL,
    byte_offset INTEGER NOT NULL,
    size        INTEGER NOT NULL,
    cv          TEXT,
    state       TEXT NOT NULL DEFAULT 'pending',
    PRIMARY KEY (file_id, idx)
) STRICT;

CREATE TABLE facts (
    root              TEXT NOT NULL,
    kind              TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    value             BLOB NOT NULL,
    extracted_at      TEXT NOT NULL,
    run_id            INTEGER REFERENCES tool_runs(id),
    PRIMARY KEY (root, kind, extractor_version)
) STRICT;

CREATE TABLE file_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    file_id     TEXT NOT NULL REFERENCES files(id),
    from_status TEXT,
    to_status   TEXT NOT NULL,
    reason      TEXT,
    occurred_at TEXT NOT NULL
) STRICT;

CREATE TABLE raw_responses (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        INTEGER NOT NULL REFERENCES tool_runs(id),
    source        TEXT NOT NULL REFERENCES sources(id),
    endpoint      TEXT NOT NULL,
    accession     TEXT NOT NULL,
    body          BLOB,
    body_encoding TEXT NOT NULL CHECK (body_encoding IN ('zstd','none','hash_only')),
    body_hash     TEXT NOT NULL,
    body_size     INTEGER NOT NULL,
    fetched_at    TEXT NOT NULL
) STRICT;

CREATE TABLE discrepancies (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        INTEGER NOT NULL REFERENCES tool_runs(id),
    experiment_id TEXT REFERENCES experiments(id),
    file_id       TEXT REFERENCES files(id),
    field         TEXT NOT NULL,
    declared      TEXT,
    observed      TEXT,
    severity      TEXT NOT NULL CHECK (severity IN ('fatal','blocking','advisory')),
    resolved      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
) STRICT;

CREATE TABLE reference_assets (
    id                 TEXT PRIMARY KEY,
    experiment_id      TEXT NOT NULL REFERENCES experiments(id),
    asset_kind         TEXT NOT NULL CHECK (asset_kind IN ('fasta','transcripts','gtf','gff3','blacklist','proteome')),
    assembly           TEXT NOT NULL,
    release            TEXT,
    release_confidence TEXT NOT NULL,
    chromosome_naming  TEXT,
    seqcol_digest      TEXT,
    coord_digest       TEXT,
    file_id            TEXT NOT NULL REFERENCES files(id)
) STRICT;

CREATE TABLE experiment_references (
    experiment_id TEXT NOT NULL REFERENCES experiments(id),
    asset_id      TEXT NOT NULL REFERENCES reference_assets(id),
    PRIMARY KEY (experiment_id, asset_id)
) STRICT;

CREATE TABLE derived_files (
    id               TEXT PRIMARY KEY,
    path             TEXT NOT NULL,
    format           TEXT,
    blake3_root      TEXT,
    classification   TEXT NOT NULL,
    builder          TEXT NOT NULL,
    builder_version  TEXT NOT NULL,
    params_hash      TEXT NOT NULL,
    built_at         TEXT NOT NULL,
    run_id           INTEGER NOT NULL REFERENCES tool_runs(id)
) STRICT;

CREATE TABLE derived_parents (
    derived_id  TEXT NOT NULL REFERENCES derived_files(id),
    parent_kind TEXT NOT NULL CHECK (parent_kind IN ('file','derived')),
    parent_id   TEXT NOT NULL,
    PRIMARY KEY (derived_id, parent_kind, parent_id)
) STRICT;

CREATE TABLE chipseq_samples (
    sample_id       TEXT PRIMARY KEY REFERENCES samples(id),
    target          TEXT,
    input_sample_id TEXT REFERENCES samples(id),
    antibody        TEXT
) STRICT;

CREATE TABLE rnaseq_samples (
    sample_id    TEXT PRIMARY KEY REFERENCES samples(id),
    treatment    TEXT,
    strandedness TEXT,
    time_point   TEXT
) STRICT;

CREATE TABLE singlecell_samples (
    sample_id    TEXT PRIMARY KEY REFERENCES samples(id),
    library_type TEXT,
    chemistry    TEXT,
    barcode_read TEXT
) STRICT;

CREATE TABLE tabular_samples (
    sample_id   TEXT PRIMARY KEY REFERENCES samples(id),
    kind        TEXT,
    row_count   INTEGER,
    column_hash TEXT
) STRICT;
```

Schemas of `atacseq_samples`, `methylation_samples` and `wgs_samples` to be defined when each profile is implemented.

`study_meta` holds: `schema_version`, `study_id`, `classification`, `salt`, `canary_hash`, `created_at`. The `salt` never leaves here.

`argv_shape` holds the shape of the command with values removed (`fetch --config <path> --workers <int>`), never the values.

### Facts and assertions

`facts` is indexed by the BLAKE3 root of the content, not by file. A fact is a pure function of `(content, extractor, version)`: it is never recomputed while the root and the extractor version do not change, and two files with the same content share facts. `value` is canonical JSON.

Fact kinds of v1:

| `kind`        | Content                                                      |
| ------------- | ------------------------------------------------------------ |
| `digests`     | MD5, SHA-256, size                                           |
| `structure`   | detected format, valid or not, reason, offset of the problem |
| `records`     | count of records and bases, length histogram                 |
| `read_ends`   | first and last read ID, without pair suffix                  |
| `seq_names`   | sampled sequence names; inferred chromosome convention       |
| `seqcol`      | Sequence Collection digest, the digest of `names`, `lengths` and `sequences`, and the count of sequences (`sorted_name_length_pairs` arrives with `compat`, Slice 6) |
| `gtf_header`  | pragmas of the GTF/GFF3 header                               |
| `read_header` | instrument, run, flowcell and lanes observed in the read IDs |
| `barcode`     | R1 length and hit rate against chemistry whitelists          |
| `matrix`      | column names, row count, gene ID namespace                   |

Assertions have no table. They are comparisons computed on demand between facts, `*_expected` columns and declarations of the TOML. An assertion that fails becomes a row in `discrepancies`. A file's custody state is a function of the set of assertions; there is no custody column updated by hand.

`file_groups` holds the chaining values (CVs) of the nodes of the BLAKE3 tree at the 8 MiB level. The complete outboard, in `outboard_path`, is reconstructible from them. The root derived from the CVs is equal to `blake3_root`; a divergence is `fatal`.

### Indexes

```sql
CREATE INDEX idx_files_status        ON files(status);
CREATE INDEX idx_files_sample_id     ON files(sample_id);
CREATE INDEX idx_files_verified      ON files(last_verified_at);
CREATE INDEX idx_files_pair          ON files(pair_id);
CREATE INDEX idx_groups_file         ON file_groups(file_id, state);
CREATE INDEX idx_files_root          ON files(blake3_root);
CREATE INDEX idx_samples_exp_id      ON samples(experiment_id);
CREATE INDEX idx_samples_role        ON samples(role, experiment_id);
CREATE INDEX idx_raw_responses_acc   ON raw_responses(accession, source);
CREATE INDEX idx_discrepancies_open  ON discrepancies(resolved, severity);
CREATE INDEX idx_events_file         ON file_events(file_id, occurred_at);
CREATE INDEX idx_events_run          ON file_events(run_id);
CREATE INDEX idx_derived_parents_rev ON derived_parents(parent_id);
CREATE UNIQUE INDEX idx_files_path_unique ON files(path) WHERE path IS NOT NULL;
CREATE UNIQUE INDEX idx_files_part_unique ON files(path_part) WHERE path_part IS NOT NULL;
```

---

## 4. File state machine

Each transition is a new row in `file_events`, with `run_id`. Never a destructive UPDATE.

| From        | To          | Trigger                                            |
| ----------- | ----------- | -------------------------------------------------- |
| pending     | queued      | enqueued                                           |
| queued      | downloading | worker picked it up                                |
| downloading | verifying   | bytes complete                                     |
| downloading | failed      | network/server error                               |
| verifying   | validating  | global checksum matches                            |
| verifying   | failed      | checksum does not match                            |
| validating  | done        | format and pair invariants match                   |
| validating  | invalid     | format or pair does not match                      |
| failed      | retrying    | within the attempt limit                           |
| retrying    | downloading | new attempt                                        |
| failed      | abandoned   | attempts exhausted                                 |
| done        | stale       | reverification detected a change                   |
| stale       | verifying   | revalidation triggered                             |
| invalid     | verifying   | revalidation triggered                             |
| done        | superseded  | source published different content at the same URL |
| pending     | adopted     | `adopt` registered a pre-existing file             |
| adopted     | verifying   | content verification triggered                     |

Only `done` counts toward completeness. The table is the code: the map of valid transitions is a variable and the property test reads from it.

---

## 5. Resume contract

Input: a `File` with an existing `path_part` and `file_groups`.

1. `stat` on the `.part` → actual size on disk.
2. Re-read from disk the groups marked `verified` and recompute their CVs. Safe point: the end of the longest contiguous prefix of groups whose recomputed CV matches AND ≤ size on disk.
3. Truncate the `.part` to the safe point.
4. Request `Range: bytes=<safe_point>-`, with `If-Range` of the `etag` or `last_modified` when available.
5. Response `200` → `RangeUnsupportedError`, do not write.
6. Response `412` or divergent ETag → file swapped at the origin: transition to `superseded`, `blocking`, do not write.
7. Response `206` → write from the safe point, computing the CV of each group, marking each group `verified` when it closes.
8. On the first group written in the session, check the magic bytes of the declared format. Divergence → `fatal`, `.part` preserved.
9. On completion: MD5, SHA-256 and the format extractors (§7) must have seen the whole file. Uninterrupted download: they ran during the streaming of step 7, with no re-read. Resumed download: their state does not survive between sessions, so a read of the whole `.part` feeds them. The root derived from the CVs must equal the computed root. Matches `md5_expected`/`sha256_expected` → `validating`.
10. In `validating`: structure assertions, pair assertions if there is a `pair_id`, and count assertions if there is `reads_expected`/`bases_expected`. All match → rename `.part` → final, `done`.

The source publishes `base_count` and `read_count` per run, not per file, and `read_count` does not always count what one file holds (`RELIABILITY.md` M15). So `reads_expected` and `bases_expected` hold a value only when the source declares it for that file or the user passes `--expected`. The comparison against the run's `base_count` happens once every file of the run is in `validating`, over the sum of their bases.

On the uninterrupted path, the file is read exactly once: while it arrives from the network. Any additional read outside the resume case is a bug.

Invariant: after any interruption and resumption, the final global checksum always matches. Guaranteed by a property test.

`ENOSPC` at any point: write the safe offset **before** propagating the error.

---

## 6. `adopt` contract

Registers a file that arrived from outside — `prefetch`, Globus, a physical disk, an institutional share. It is the main path for controlled data.

Input: path on disk, source, experiment, sample, declared format, expected checksum (optional).

1. Check that the path is under the study root. Outside → error.
2. `stat`: size and mtime.
3. A single streaming read producing the group CVs, root, outboard, MD5, SHA-256 and format facts.
4. If the root already has facts in `facts` with the current version of the extractors, the format extraction is skipped.
5. Structure assertions and, if there is a declaration, count assertions.
6. Register `origin='adopted'`, `url = NULL`, `path` relative to the root, verification baseline.
7. Final state: `done` if there was an expected checksum and it matched; `adopted` if there was no expected one; `invalid` if it matched and failed.

The file is **neither copied nor moved**. Adoption is registration, not transfer.

Without `url`, there is no resume — there is nowhere to resume from. Reverification and scrub work identically to downloaded files, which is the entire purpose.

`colligo adopt --manifest list.csv` adopts in batch from a CSV with columns `path,sample,format,md5`.

---

## 7. Verification funnel

```
verify(file):
    if baseline exists and mode != deep and current stat == baseline:
        return UNCHANGED
    read file in a 1MB buffer, recomputing the group CVs
    if any CV diverges from file_groups: mark diverging groups; status invalid; return list
    root matches blake3_root
    for each fact kind:
        if facts has (root, kind, current version): reuse
        else: extract in the same read
    evaluate assertions
    update last_verified_at and baseline
    return VERIFIED
```

`UNCHANGED` is not `VERIFIED`. The `stat` gate decides what needs to be read; it does not update `last_verified_at` nor take the custody state to `verified`, because silent disk corruption changes neither size nor mtime.

The same hash implies the same structure: with the root matching, format facts are only re-extracted when the extractor version changes.

Modes: `--trust-mtime` (gate only, never produces `VERIFIED`), `--deep` (always reads the content; reuses facts if the root matches, unless `--reextract`).

Scrub: selects by number of dependent results descending (count in Scribo's `result_inputs`, when it exists), then by `last_verified_at` ascending; I/O ceiling per run; covers the collection in an amortised way. Verifying first the file that the most results depend on covers the greater risk with the same I/O.

Verification concurrency: default 1–2, configurable, rises only on flash storage.

`check`, `validate` and `verify` standalone, stateless, with no database: accept a loose file with `--expected`, a checksums file, or `--source` (Slice 2) to compare against the source. `--cross-check` emits output compatible with `sha256sum` and `b3sum`.

### Single read

One read of the file feeds all the consumers: BLAKE3 tree, MD5, SHA-256, and the extractor of the declared format, which receives the already decompressed stream when the format is compressed. No consumer reads the file on its own.

Distribution in parallel, not in series. `io.MultiWriter` calls each consumer in sequence on the same goroutine, and the cost becomes the sum of all of them; it is forbidden on this path. The reader fills buffers of a reused ring and hands the same slice, read-only, to one goroutine per consumer. The buffer goes back to the ring when all have acknowledged. Total cost of the read: that of the slowest consumer, not the sum. See §12, "Performance".

Before any verification: known hash vectors and the study canary. See `RELIABILITY.md` §M9.

---

### Discovery and diagnosis

`check .` and `get` classify each file by the magic bytes and the first line, never by the extension alone; a divergence between extension and content is `advisory`. Pairs are formed by suffix (`_R1`/`_R2`, `_1`/`_2`, `.1`/`.2`, with the chunk number `_001` when there is one, which must be equal in both) and confirmed by the `read_ends` facts; a pair whose suffix matches but whose IDs do not is `blocking`.

Every assertion that fails references an entry of the `diagnoses/` catalogue, versioned with the code. Each entry has: identifier, failure mode of `RELIABILITY.md`, observation sentence with the values, probable cause, suggested action. The readable output prints the sentence; `--json` includes the identifier. An assertion without an entry in the catalogue is a bug and fails the suite.

---

## 8. Writer goroutine

```
workers → event channel (each event + chan error) → writer → SQLite
```

A single writer serialises writes. The worker sends an event and waits for the ack. A write error propagates and aborts the pipeline.

Every event carries the current `run_id`.

Shutdown: cancel context → stop accepting events → drain channel → final commit → fsync → write `ended_at` and `exit_code` in `tool_runs` → exit.

Batch: 100 events or 50ms, whichever comes first, with a forced commit on cancellation. `fatal` transitions are committed immediately, without batching.

---

## 9. Public interfaces

Adapters are **pure**: they do not receive a `Store`, do not receive a disk path, do not open files, do not write. The metadata part of an adapter — `Fetch` and `Resolve` without download — is what the light door uses in `check --source` (Slice 2) and what `recheck --upstream` re-runs.

The extractor receives the content through `Write`, in the single read, and returns facts in `Finish`. It does not open files, does not seek, does not read twice. A format whose validation requires random access is modelled as an extractor that accumulates only what is necessary — the BGZF EOF block is the last 28 bytes.

**Substitution boundary.** The extractor is the only piece that may have an implementation outside Go. Contract of an external extractor:

- Executable on the PATH named `colligo-<format>`; Colligo discovers it, never requires it.
- Receives the content on stdin and the format and the expected `extractor_version` as arguments.
- Writes the facts to stdout as canonical JSON, in the same schema as the `facts` table, and exits with 0; any other output is an error of the extractor, not of the file.
- A version different from the expected one → Colligo refuses and uses the Go extractor.
- Never reads a path, the network, config or the database; never writes a file.

Every external extractor goes through the differential test against the Go extractor of the same version before being accepted in the suite.

```go
type Source interface {
    ID() string
    Fetch(ctx context.Context, accession string) (Fetched, error)
    Resolve(ctx context.Context, s model.Sample) ([]model.Artifact, error)
}

type Fetched struct {
    Experiment model.Experiment
    Samples    []model.Sample
    Raw        []model.RawResponse
    Issues     []model.ValidationIssue
}

type Sample struct {
    ID           string
    ExperimentID string
    Accession    string
    Layout       string
    SourceStatus string
    Declared     RunCounts
    Artifacts    []Artifact
}

type RunCounts struct {
    ReadCount *int64
    BaseCount *int64
}

type Artifact struct {
    URL          secret.Endpoint
    Name         secret.Name
    Size         int64
    MD5          string
    SHA256       string
    Format       string
    PairID       string
    SourceStatus string
    Headers      map[string]string
}

type Transport interface {
    Get(ctx context.Context, url secret.Endpoint, rng *ByteRange) (*Response, error)
}

type ByteRange struct {
    Start int64
    End   int64
}

type Response struct {
    Status        int
    ContentType   string
    ContentLength int64
    Body          io.ReadCloser
}

type FormatValidator interface {
    Format() string
    Version() string
    MagicPrefix() []byte
    Compressed() bool
    NewExtractor() Extractor
}

type Extractor interface {
    io.Writer
    Finish() ([]model.Fact, error)
}

type Store interface {
    BeginRun(ctx context.Context, meta RunMeta) (int64, error)
    EndRun(ctx context.Context, runID int64, exitCode int) error
    SaveExperiment(ctx context.Context, exp model.Experiment) error
    SaveSample(ctx context.Context, s model.Sample) error
    SaveFile(ctx context.Context, f model.File) error
    SaveGroups(ctx context.Context, fileID string, groups []model.Group) error
    UpdateFileStatus(ctx context.Context, id string, status model.Status, reason string) error
    UpdateGroupState(ctx context.Context, fileID string, idx int, state string) error
    GetPendingFiles(ctx context.Context) ([]model.File, error)
    GetGroups(ctx context.Context, fileID string) ([]model.Group, error)
    SaveFacts(ctx context.Context, root string, facts []model.Fact) error
    Facts(ctx context.Context, root string) ([]model.Fact, error)
    UpdateSourceStatus(ctx context.Context, sampleID string, status string) error
    SaveRawResponse(ctx context.Context, r model.RawResponse) error
    SaveDiscrepancy(ctx context.Context, d model.Discrepancy) error
    FilesForVerification(ctx context.Context, limit int) ([]model.File, error)
    RegisterDerived(ctx context.Context, d model.Derived, parents []model.ParentRef) error
    Completeness(ctx context.Context) (model.Completeness, error)
    Classification(ctx context.Context) (string, error)
}

type Profile interface {
    Type() string
    ValidRoles() []string
    RequiresControl() bool
    LinkStrategy() LinkStrategy
    Validate(s model.Sample) []model.ValidationIssue
}

type Logger interface {
    Debug(msg string, fields ...logf.Field)
    Info(msg string, fields ...logf.Field)
    Warn(msg string, fields ...logf.Field)
    Error(msg string, fields ...logf.Field)
}
```

`transport.Client` is the HTTP implementation, for metadata only. `Get` accepts only a `nil` `ByteRange` until the resume slice and answers anything else with `RangeNotImplementedError`. `Response.Body` is bounded by a size limit (16 MiB by default) and a request has a timeout (30 s per attempt). A 2xx answer whose media type is not in the allowed list (text, TSV, CSV, JSON, XML by default) is a `ContentTypeError`, never a body: an HTML page served where a TSV was expected must not reach a parser. `Reveal` on `secret.Endpoint` is called only there.

A `Sample` is one run. `Declared` holds the totals the source publishes for the whole run, summed over its files: `ReadCount` and `BaseCount` are not per file, and `Artifact` has no field for them. `Artifact.PairID` is the run accession when the file name carries a `_1` or `_2` mate suffix, and empty for a file without one. `Layout` is the source's own label, never verified by the adapter. `Fetch` fills `Raw` even when it also returns an error. An empty field does not drop the run: an empty `read_count` or `base_count` is `nil`, not declared, which is not zero, so a comparison against it is skipped rather than failed; an empty `library_layout`, `status` or count is an `advisory` `ValidationIssue`; and a run whose three file columns are all empty is kept with no artifacts and its status, which is how a suppressed run must reach the source checks (`RELIABILITY.md` M16). Only an empty `run_accession`, a malformed value, misaligned file lists or file columns that are only partly empty make a row defective: a blocking `ValidationIssue`, and the run is left out of `Samples`; a response that is not a table, lacks a column or lists no usable run is a `*model.SourceError`; an accession with no row is a `*model.NotFoundError`.

`SaveRawResponse` decides on its own whether to store the body or only the hash, consulting the classification. The caller has no choice.

Construction via functional options:

```go
store, err := colligo.Open("study.db",
    colligo.WithLogger(logger),
    colligo.WithSources(gencode.New(), geo.New(geo.WithAPIKeyEnv("COLLIGO_NCBI_API_KEY"))),
)
```

Iteration over large studies via `iter.Seq2`.

`sourcetest` and `storetest` are public API. Adding an assertion is a semver event.

---

## 10. Typed errors

```go
type ChecksumError struct{ ... }
type GroupMismatchError struct{ ... }
type FormatError struct{ ... }
type PairMismatchError struct{ ... }
type IdentityMismatchError struct{ ... }
type SupersededError struct{ ... }
type SourceError struct{ ... }
type TypeMismatchError struct{ ... }
type NotFoundError struct{ ... }
type AuthError struct{ ... }
type RangeUnsupportedError struct{ ... }
type DiskSpaceError struct{ ... }
type IncompleteError struct{ ... }
type CountMismatchError struct{ ... }
type UpstreamChangedError struct{ ... }
type IncompatibleError struct{ ... }
type ClassificationError struct{ ... }
type SelfTestError struct{ ... }
type ClockError struct{ ... }
type StatusError struct{ ... }
type ContentTypeError struct{ ... }
type ResponseTooLargeError struct{ ... }
type RetryAfterError struct{ ... }
type RetryExhaustedError struct{ ... }
type RangeNotImplementedError struct{ ... }
type UnsupportedEndpointError struct{ ... }
```

Inspectable via `errors.As`. On the light door, the failure of a file is not one of these errors: it is an assertion of the `diagnoses/` catalogue with a severity, and `check` does not abort because of it. Today only `SelfTestError` (`checksum/`) and `samplesheet.FormatError` (a samplesheet that cannot be read, a usage error) exist; the others arrive with the `store` and the slices that use them. Every path, URL or credential field uses the types of `secret/` — including `RangeUnsupportedError`, `SourceError` and `ChecksumError`, which today carry `string`. Every error carries `Severity()`.

---

## 11. Network error strategy

| Error                      | Strategy                                |
| -------------------------- | --------------------------------------- |
| Timeout / 5xx              | Retry with exponential backoff + jitter |
| 429 / 503 with Retry-After | Honour the header above its own backoff |
| Diverging group CV         | Re-download only the group              |
| 200 when 206 was expected  | Permanent (`RangeUnsupportedError`)     |
| 412 or diverging ETag      | `SupersededError`, blocking             |
| 404                        | Permanent                               |
| 401 / 403                  | Permanent, warns the user               |
| ENOSPC                     | `fatal`, after writing the safe offset  |

Backoff: `min(2^N, 300s) + jitter`. Rate limiter shared per API key.

The metadata client applies the rows that concern a GET: timeout and 5xx retry with the backoff above (1 s, 2 s, 4 s, up to 4 attempts, jitter at most a quarter of the delay); 429 and 5xx honour `Retry-After` when it is larger than the backoff, in seconds or as an HTTP date; a `Retry-After` above the 300 s cap is a `RetryAfterError` and the client neither waits nor retries; 404 and 410 are `NotFoundError`, 401 and 403 are `AuthError`, any other 4xx is a `StatusError`, none of them retried. When retries run out the last error is wrapped in `RetryExhaustedError`. Errors and logs carry the endpoint through `secret.Endpoint`, so the query string never appears.

---

## 12. Concurrency

```
Metadata pool  → 20–50 goroutines, rate limited per domain
Download pool  → 4–8 goroutines, connection limit per domain
Verify pool    → 1–2 goroutines
```

Per-domain limits (default, configurable): `ebi.ac.uk` (the ENA Portal API and `ftp.ebi.ac.uk`) 4, `ftp.ensembl.org` 2, `ncbi.nlm.nih.gov` 3, `www.cbioportal.org` 2, `files.rcsb.org` 4, `dfam.org` 2.

For the metadata pool the number is a ceiling of requests per second: the client spaces the start of consecutive requests to a domain by at least `1/N` of a second, retries included, so no window of one second holds more than `N` starts. A domain matches itself and its subdomains, the longest match wins, and every host under it shares that one ceiling (`www.ebi.ac.uk` and `ftp.sra.ebi.ac.uk` together stay under the 4 of `ebi.ac.uk`); a host without an entry gets 2 of its own. `WithRate` and `WithDefaultRate` change it.

These defaults exist so as not to burn your institution's IP. Raising them is your responsibility and is recorded in `config_hash`.

HTTP: `Accept-Encoding: identity` for already compressed files; connection reuse via `MaxIdleConnsPerHost`; 1MB `io.CopyBuffer`. File content is never in memory.

Local read concurrency adjusted to the storage: several files at the same time on SSD and network filesystems; one at a time on a rotational disk, where concurrent reads turn into random seeks. Conservative default of 2; `--jobs` changes it. Today the default is 2 on any storage: detection of a rotational disk does not exist yet.

### Performance

Order of cost, from largest to smallest: network, decompression, parser, hash, database. The database and the `modernc` driver are never the bottleneck.

**Physical floor.** Validating plain gzip and FASTQ requires decompressing everything. The floor of a full `check` is pure-Go decompression (`klauspost/compress/gzip`) of the same file. Goal: a full `check` of a `.fastq.gz` costs at most 10% above that floor on the same machine — this measures what is ours: single read, hashes and parser. `zcat file | wc -l` is still measured and reported; the distance to it is that of Go's inflate against C's zlib, and closing it requires cgo or speculative decompression (`BACKLOG.md`), which do not enter at the expense of reliability. Measured in the benchmark of `TESTING.md` §7.1.

**Decompression.**

- `github.com/klauspost/compress/gzip` in place of `compress/gzip`, conditioned on a benchmark showing a gain on the reference machine.
- BGZF and BAM: independent blocks, decompressed in parallel by a bounded pool, with reordering before the extractor. A Slice 7 goal (`ROADMAP.md`); today BGZF goes through the same sequential decompressor as gzip.
- Plain gzip has no trivial parallelism. Speculative parallel decompression, like rapidgzip's, is in `BACKLOG.md`.

**Allocation-free FASTQ parser.** No line-by-line `bufio.Scanner`. Line counting by `bytes.IndexByte` over the buffer itself; sum of the length of the sequence lines; check that quality and sequence have the same length; a record split between two buffers handled by a remainder of at most one line. Zero allocations per record, verified by `testing.AllocsPerRun`.

**Not doing.** Facts indexed by root make a rerun over unchanged files one `stat` per file. The second run is measured and has its own goal.

**Multi-connection download.** A large file can be downloaded by several `Range` slices in parallel, each aligned to 8 MiB groups with its own CV, without exceeding the domain's connection limit. Default: 1 connection per file; `--connections` raises it. Slices finish out of order; the file only advances to `verifying` when all groups are `verified`, and MD5/SHA-256, which are sequential, are computed on the final read as in the resume case.

**Verification overlapped with the download.** The downloader does not write and then re-read: each buffer that arrives from the network goes, at the same time, to the `.part` and to the consumers of the single read (§7). When the last byte arrives, the facts are ready. The only re-read is that of the resume case (§5, step 9). Goal: `get` with full verification ≤ 105% of the time of plain `curl` for the same file and server.

**Adaptive concurrency per domain (AIMD).**

- Starts with 1 connection per domain.
- At each 5 s window, if the domain's aggregate throughput grew by more than 5% since the previous window, add 1 connection (additive increase), up to the ceiling.
- A 429 or 503 response, timeout, connection reset, or throughput drop above 20% between windows: divide the number of connections by 2, minimum 1 (multiplicative decrease). `Retry-After` is honoured above everything.
- The ceiling is the per-domain limit of this section, configurable. The defaults stop being a fixed value and become a ceiling.
- Each decision is recorded in a `debug` log with the observed throughput; the per-domain summary goes to `tool_runs`.

**Honest quick mode.** `check --quick` reads only magic bytes, the end of the file (the BGZF EOF block; in plain text, whether the last byte is a newline) and a sample of the start. For plain gzip there is nothing to check at the end without decompressing everything: CRC and ISIZE only match against the content. It takes seconds. The result is `quick`, never `verified`, and the exit code is 5 when nothing is found (`RELIABILITY.md` §1): the report says the content was not read in full, and no custody state is updated from it.

---

## 13. Manifest and attestation

`colligo manifest export --out m.json` produces JSON with:

- `schema_version`, `study_id`, `classification`, `redacted`, `created_at`
- `tool`: version, git SHA
- `files[]`: `id`, `name` or `opaque_id`, `size`, `format`, `group_size`, `blake3_root`, `outboard` (reference to the outboard file that accompanies the manifest), `md5`, `sha256`, `reads`, `bases`, `custody_state`, `last_verified_at`
- `derived[]`: `id`, `builder`, `builder_version`, `params_hash`, `parents[]`
- `references[]`: `assembly`, `release`, `release_confidence`, `chromosome_naming`, `seqcol_digest`, `coord_digest`, `asset_kind`

It never contains: salt, credential, URL of a restricted study, real name of a restricted study.

`colligo attest --manifest m.json --root dir` compares the local collection against the manifest and reports, per file: missing, size divergent, diverging groups with indexes, or conforming. The verification is slice against root: whoever attests does not need to trust whoever produced the manifest beyond the root. Readable output and `--json`. Exit code 0 if everything conforms, 3 if not.

Attestation works with a redacted manifest, because hashes are not sensitive.

### Certificate

`colligo check --certificate` writes `<file>.colligo-cert.json` with: schema version, tool version, size, `blake3_root`, `group_size`, MD5, SHA-256, facts by `kind` with `extractor_version`, and each assertion evaluated with declared, observed and result.

The repository keeps a reference verifier in `tools/verify_cert.py`: Python with the standard library plus the `blake3` package, importing nothing from Colligo. It recomputes size, hashes and the `records` facts for FASTQ, checks them against the certificate and also checks the assertions: `checksum_mismatch`, `size_mismatch`, `count_mismatch` and `structure_invalid` must agree with what it recomputes, including `passed`. The BLAKE3 root of a file with more than one group is checked in full; the outboard is not checked because the certificate does not carry it. A divergence between verifier and certificate is a bug in one of the two implementations and fails the suite. Exit codes of the verifier: 0 agrees, 1 disagrees, 2 cannot judge (usage, unreadable input, unknown `schema_version`, missing required field or unknown field) — "I don't know" never becomes "I disagree" nor "I agree". On agreeing, the verifier then prints a line `UNCHECKED fact <kind> (<extractor version>)` or `UNCHECKED assertion <id>` for each fact and assertion it did not recompute, so that `AGREE` is not read as more than it is. Adding a field to the certificate requires bumping `schema_version`: the verifier exits 2 on an unknown field, so a new field without a new version would make every existing verifier refuse the certificate.

The certificate of a restricted study follows the table of `CONFIDENTIALITY.md` §5.7: opaque name, no URL.

### Lockfile

`colligo lock` generates `colligo.lock`: the manifest reduced to what an analysis needs to pin, in text, sorted and stable, made to be versioned in the analysis repository — the `go.sum` of data.

One logical line per entry: relative name, `blake3_root`, size, format, and the confirmed identities (`release`, `seqcol_digest`, `coord_digest`, `reads`, `bases`). No timestamps, so that the diff between two commits shows only what actually changed.

`colligo check --lock colligo.lock` at the start of the pipeline checks the local collection against the lock and fails closed. It does not require a database: it is the light door. The diff of the lock between two versions of the analysis shows exactly which entry changed — a release from 44 to 45, a replaced file.

---

## 14. Configuration

Precedence: defaults < TOML < env < flags.

```toml
[study]
name = "my_study"
db   = "study.db"
mode = "strict"

[sources.gencode]
classification = "public"
rate_limit     = 2

[sources.geo]
classification  = "public"
rate_limit      = 3
credential_env  = "COLLIGO_NCBI_API_KEY"
email           = "researcher@institution.org"

[sources.dbgap]
classification     = "restricted"
credential_command = "pass show dbgap/token"

[download]
workers   = 8
retry_max = 5
resume    = true

[verify]
workers      = 2
scrub_window = "90d"
scrub_io_cap = "20GB"

[upstream]
recheck_window = "30d"

[cache]
experiment_metadata_ttl = "30d"
file_status_ttl         = "1d"
reference_metadata_ttl  = "90d"
raw_response_ttl        = "90d"

[reference]
assembly = "GRCh38"
release  = "GENCODE 44"

[results]
table  = "rnaseq_qc"
fields = ["Mapped_Reads", "Alignment_Rate", "Assigned_Reads"]

[experiments.GSE116256]
type   = "rnaseq"
source = "geo"
```

`[results]` is optional: it declares Scribo's table and the fields expected per sample, which extend completeness up to "processed" (`CONTRACT.md` §7).

`classification` is mandatory per source. Absence → error naming the source.

A credential in TOML → error naming the key and the line. See `CONFIDENTIALITY.md` §5.4.

Env: `COLLIGO_DB_PATH`, `COLLIGO_LOG_LEVEL`, `COLLIGO_SYNC_DIRS`, and the credential variables declared per source.

As a lib, secrets via options; the lib has no opinion on the origin of the secret.

---

## 14.1 Integration with Scribo

Per `CONTRACT.md`. Colligo owns `samples`, `files` and `facts`, which Scribo reads. Colligo reads, and never writes, `result_inputs` and `input_signals`.

**Signals in custody.** When evaluating the assertions of a file, the `input_signals` signals with the same root enter as additional assertions:

| Signal               | Assertion                                                      | Severity   |
| -------------------- | -------------------------------------------------------------- | ---------- |
| `suspect`            | `post_pipeline_consistency` failed, with `source` and `detail` | `advisory` |
| `identity_confirmed` | `identity` passed                                              | —          |
| `identity_mismatch`  | `identity` failed                                              | `blocking` |

The original custody state is never overwritten. `status` lists suspect files with the source of the signal; the certificate includes the `identity` assertion when it exists.

**Completeness funnel.** `colligo status` shows declared → downloaded → verified → processed. "Processed" requires `[results]` in the TOML and, for each sample, the declared fields filled in Scribo's table and not stale per the graph of `CONTRACT.md` §6. `--require-complete` considers the whole funnel when `[results]` exists.

**`gc` with provenance.** When implemented (`BACKLOG.md`), it classifies each file by `result_inputs`: unused; used and regenerable from inputs under custody; used and irreplaceable.

---

## 15. Source URIs

Besides `https` and `ftp`, the URL field accepts `drs://` (GA4GH Data Repository Service), resolved by a configured resolver. A resolved DRS produces a normal access URL and inherits the whole integrity path. Low cost, and it puts Colligo in a vocabulary that consortium tools recognise.

---

## 16. CLI

Implemented today: `check`, `validate`, `identify` (FASTA and GTF) and `version`, with the flags `--json`, `--permissive`, `--recursive`, `--format`, `--jobs`, `--expected`, `--quick`, `--cross-check`, `--certificate`, `--samplesheet` and `--source` (ENA only). The "Slice" column says when each command or flag of the rest arrives (`ROADMAP.md`); "—" is what does not have a slice yet.

`--recursive` descends into subdirectories (without it, only the given directory; names that start with `.` are ignored, and a symbolic link to a directory is never followed). `--format` declares the format (`fastq`, `fasta`, `gtf`, `gzip` or `bgzf`). `--jobs` is the number of files read at the same time.

**Light door** — no database, no TOML, no state. It does not import `store`.

| Command                 | Slice | Function                                                                                                                         |
| ----------------------- | ----- | -------------------------------------------------------------------------------------------------------------------------------- |
| `check`                 | 1     | facts + assertions of loose files; `--json`                                                                                      |
| `validate`              | 1     | format structure only                                                                                                            |
| `check .`               | 1     | discovers formats by content and pairs by name; zero configuration                                                               |
| `check --quick`         | 1     | only magic bytes, end of file and sample of the start; never `verified`; exits 5                                                 |
| `check --certificate`   | 1     | emits a certificate verifiable by third parties (§13)                                                                            |
| `check --source`        | 2     | compares against the source: ENA `read_count`, `base_count`, status, MD5 and size (implemented, see below)                       |
| `check --samplesheet`   | 2     | checks an nf-core pipeline samplesheet against the content: files exist, R1 and R2 match, counts match, samples are not repeated (implemented, see below) |
| `identify`              | 2     | identity by content: seqcol (FASTA), GENCODE GTF header, hash against official checksums (implemented, see below)                |
| `get`                   | 3     | accession → download with overlapped verification → `samplesheet.csv` + `colligo.lock`; no database                              |
| `lock`                  | 4     | generates `colligo.lock` from loose files or from a study                                                                        |
| `check --lock`          | 4     | checks the collection against a `colligo.lock`                                                                                   |
| `compat`                | 6     | compares coordinate systems between FASTA, `.fai`, BAM, VCF, GTF, BED                                                            |
| `verify`                | —     | standalone with `--expected` or a checksums file; today `check --expected` and `check --cross-check` cover the use               |
| `check --cross-extract` | —     | runs the Go and an external extractor on the same file and compares the facts; only with an external extractor present           |

`check --samplesheet <file.csv>` takes the samplesheet as the flag value and no other path; `--quick`, `--expected`, `--format` and `--recursive` do not apply and are usage errors. `--json`, `--permissive`, `--jobs`, `--cross-check` and `--certificate` work as in `check`, over the files the samplesheet lists: `colligo.sha256` and `colligo.b3` are written in the working directory with each path as resolved, so `sha256sum -c` runs from there whichever directories the files are in; certificates go next to each file; a remote entry is in neither.

- **Formats.** Two, checked against the schema of nf-core/rnaseq 3.27.0 (`assets/schema_input.json`). The nf-core/rnaseq format has the columns `sample`, `fastq_1`, `strandedness` (`forward`, `reverse`, `unstranded` or `auto`) and optionally `fastq_2`, `seq_platform`, `seq_center`, `genome_bam`, `transcriptome_bam`, `percent_mapped`; the BAM columns are not checked. The generic format is exactly `sample,fastq_1,fastq_2` in any order. Any other header, a repeated column, a malformed CSV, a sample name that is empty or has spaces, an empty `fastq_1`, a bad `strandedness` or a sheet without samples is a usage error (exit 1) naming the line; nothing was read. A UTF-8 byte order mark is accepted. Blank lines are skipped and lines are counted as an editor shows them, the header being line 1.
- **Paths.** Each path becomes a `secret.Name` when the line is read. A relative path is resolved against the directory that holds the samplesheet, not against the working directory. A value with a scheme (`s3://`, `https://`, `ftp://`...) becomes a `secret.Endpoint`, is never fetched and is reported as `sheet_remote_path`, `blocking`: nothing about that entry was verified, so the run cannot exit 0 (`--permissive` lowers it to 0, as for any `blocking`).
- **Reading.** Each distinct path is read once, however many lines name it, by the single read of §7. Mates are the ones the samplesheet declares in `fastq_1` and `fastq_2`, not the ones the names suggest, and the comparison is the same as for `check .` (`pair_count`, `pair_ids`); the failed assertion names the column and the line. A row without `fastq_2` is single-end and is not a missing mate.
- **Repetition.** Several lines with the same `sample` are lanes and are legitimate when their files differ. `sheet_duplicate_row` (`blocking`) is the same sample with the same file content in the same columns. `sheet_shared_file` (`blocking`) is the same content in two lines of different samples, in two lines of one sample that are not otherwise identical, or as `fastq_1` and `fastq_2` of one line. The comparison is by BLAKE3 root, so a copy under another name counts. `sheet_mates_swapped` (`blocking`) fires when two lines fail the pair check and each line's `fastq_1` fits the other line's `fastq_2`; it names both lines.
- **Missing file.** `read_failed`, `fatal` (exit 2), with the line where the file is listed; the other files are still checked.
- **Output.** The `--json` document is the one of `check`, with one entry per distinct file (and one per remote location); the samplesheet adds no field of its own.

`check --source ena:<accession> <path>...` reads the files as `check` does and compares them with what the ENA Portal API declares for the accession, which may be a run, an experiment, a sample or a study: every run it returns is compared. The source is queried once, after the paths are resolved and before any file is read in full. `--quick` and `--expected` do not apply (the counts need every file read in full, and the source declares the values) and are usage errors, like `--samplesheet` together with `--source` and a source other than `ena`. An accession that is not of the ENA form is a usage error (exit 1). The comparison lives in `verifier` (`CheckAgainstSource`), which receives the runs as `model.Sample` values and never imports `source/*`; the CLI only builds the adapter over the real `transport.Client`.

- **Which local file is which.** The link is made in two steps, the name first. A local file stands for the declared file whose name it has, or whose name it has once `.gz` is removed: `DRR021374_1.fastq.gz` and `DRR021374_1.fastq` both stand for `DRR021374_1.fastq.gz`. The name comes first because the case M15 exists for is a locally extracted FASTQ, whose bytes never matched the published MD5. Only a file with the exact declared name is held to the published `fastq_md5` and `fastq_bytes` (`checksum_mismatch`, `size_mismatch`, `fatal`), because a decompressed copy has other bytes by design. When an exact name and a decompressed copy both claim one declared file, the exact name stands for it and the copy gets `source_file_unlisted`. Two claims of the same kind (the same name in two directories) make every claimant `source_file_ambiguous` (`blocking`), and the run is not compared.
- **Renamed files.** After every file was read, a declared file that no name reached is linked to the unclaimed local file whose computed MD5 and size both equal the published ones (`linked_by: "md5"`). Equal MD5 and size are the integrity check of that file, so it is not held to anything further, and it enters the comparison of the run, the pair (by the declared name, never the local one) and the layout like any other file. A name always wins: when a file with the right name and a renamed copy of the same content both exist, the right name stands for the declared file and the copy gets `source_file_unlisted`. Two unclaimed files with the content of one declared file, or one file with the content of two declared files, are `source_file_ambiguous` (`blocking`): Colligo never picks one in silence. The text report says so on a line of its own (`amostra1_R1.fastq.gz stands for SRR10663037_1.fastq.gz (same MD5 and size, not the same name)`). The limit that remains: a file that is both renamed and decompressed has neither a name nor an MD5 that matches, and stays `source_file_missing` for the declared file and `source_file_unlisted` for the local one (M15).
- **Missing and unlisted.** A declared file with no local file, by name or by content, is `source_file_missing` (`blocking`, M6): the run is incomplete. A local file that no run lists is `source_file_unlisted` (`advisory`, M1): it does not make the run wrong. A renamed copy whose content changed (a shortened read 2) matches nothing, so it is `source_file_missing` and never `verified` as the run's file.
- **Status.** A run whose status is not `public` is `source_status`, `blocking` (M16): the archive condemned the data, and nothing on disk changes when it does. It is evaluated for every run, including one that lists no files, which is how a suppressed run usually comes back.
- **Bases.** The bases summed over every file of the run against `base_count` is `count_mismatch`, `blocking` (M15). It runs once per run, only when every declared file has a local file that passed its own checks; otherwise it is skipped, naming what blocked it (`source_file_missing`, `source_file_ambiguous`, or the failed assertion of the file).
- **Reads.** `read_count` is matched against three readings: every file has `read_count` reads, the files add up to it, or the confirmed pairs (counted once) plus the unpaired files add up to it. A pair is two declared files with mate suffixes that pass `pair_count` and `pair_ids`. A match passes and `observed` names the reading. No match is `read_count_unexplained`, `advisory`, never `blocking`. When the bases fail, `read_count` is skipped.
- **Layout.** A run labelled `SINGLE` whose files form a confirmed pair is `layout_mismatch`, `advisory`. A run labelled `PAIRED` with a pair passes; `PAIRED` without a pair is not judged, because Colligo does not check for interleaved files.
- **Not declared.** A count or status the source left empty is not zero: the comparison is recorded with `skipped: true`, `skipped_by: "not_declared"` and `declared: "not declared"`. A run that lists no files has its content comparisons skipped with `skipped_by: "no_files_listed"`. The `advisory` and `blocking` validation issues of the response become `source_field_empty` and `source_row_rejected` (the row was dropped, so the run could not be compared).
- **Files of a failed run.** When the bases fail or the status is not `public`, each local file of the run gets `source_run_failed`, `blocking`, so no file of a failed run reads `verified`. It is a separate assertion and not a copy of the run's: the count observed for the run is not the count of the file, and a certificate must not claim it is.
- **Failure of the source.** A network error, an HTTP error, an HTML page, a response that is not a table or an accession with no run is fatal (exit 2), with the message of the typed error, which never carries the query string, and no report: nothing was compared, so nothing is verified. Cancellation is exit 4.
- **Exit code.** The worst severity over the files, the runs and the response, as for `check`.

The `--json` output of `check` and `validate` is an object with `schema_version` (today 2), `files[]` (`path`, `status`, `container`, `content`, `size`, `blake3_root`, `md5`, `sha256`, `assertions[]`), `skipped`, `skipped_directory_links`, `source` and `exit_code`. Each assertion carries `id` (from the `diagnoses/` catalogue), `mode`, `severity`, `passed`, `skipped`, `skipped_by`, `declared` and `observed`, and, when it failed, `observation`, `probable_cause` and `suggested_action`. `source` is present only with `--source`: `id`, `accession`, `assertions[]` (the issues of the response) and `runs[]`, each with `accession`, `layout`, `status`, `declared` (`read_count`, `base_count`, `null` when not declared), `files[]` (`name` as the source lists it, `local` the file that stands for it or `null`, `linked_by` `"name"` or `"md5"` when `local` is set, `md5`, `size`) and `assertions[]`. A new field requires bumping `schema_version`; version 2 added `source`, and `linked_by`, added before version 2 was released, is part of it.

### `identify`

`colligo identify [--json] <path>...` reads each file once and names it by its content. No database, no TOML; it does not import `store`. Three signals can name a file, from the weakest to the strongest: the header of a GENCODE GTF (`declared`), the digest of a sequence collection and the checksum its source published (`identified`).

**Status.**

| `status`         | Meaning                                                                                                                                  | Exit |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ---- |
| `identified`     | the content matches something official: an entry of the checksum table or of the table of sequence collections                          | 0    |
| `declared`       | the file says what it is, in a GENCODE header, and nothing in the content contradicts it. A header can be edited: this is not a proof   | 0    |
| `conflict`       | the header says GENCODE and the content says otherwise                                                                                   | 3    |
| `not_identified` | no signal names the file, or it is a format that has no identity to derive. Not an error                                                 | 0    |
| `invalid`        | the file, or its gzip or BGZF container, has a structural problem: a truncated or broken file must not look like an answer               | 3    |
| `unreadable`     | missing, unreadable, or changed during the read                                                                                          | 2    |

An unreadable file outranks 3; 1 is a usage error; 4 is an interruption. `invalid` is decided first: a file that is not valid is never identified, whatever its header or checksum.

**Checksum.** The MD5 of the bytes of the file, as stored (a `.gz` is hashed compressed), taken from the same read, is looked up in the embedded table of official checksums (`identity.KnownChecksums`). The name of the file never takes part. The table is `identity/checksums.json`; every entry has the `name` of the release, the `file` as the source names it, the `md5`, the `origin` (the address of the checksum file it was read from) and the day it was `retrieved`. `identity.ParseChecksums` rejects an entry with no origin or no date, a malformed MD5 or an MD5 that appears twice, and `identity.Verify` runs it on the embedded table, so a bad table stops `identify` before anything is claimed. The table is built by `tools/gen_checksums.py` from the copies of the official `MD5SUMS` files kept in `tools/checksums/` (listed with their origin and date in `sources.tsv`), and a test rebuilds it and compares it byte for byte; the output is sorted and does not depend on the order of the sources. Today it holds the GTF, GFF3 and FASTA files of GENCODE human 44, 48, 49 and 50 and mouse M33, M37, M38 and M39: the three latest releases of each organism on 2026-10-31, plus the ones checked on real files. GENCODE compresses every release anew, so even a genome FASTA whose sequences did not change has a different MD5 in each release, and no MD5 repeats across them: the checksum names the exact file of one release, while the sequence collection digest (the same for every release of one assembly) names its content. A match is `identified` for any format and is reported in `checksum`.

**GENCODE GTF.** The extractor keeps the leading comment lines of the GTF, up to 1,024 lines or 1 MiB (whichever comes first, counted on the real length of each line) and 1 KiB per value, and the first occurrence of `##description`, `##provider`, `##format` and `##date`. A header that outgrows the limit is flagged (`header_truncated`) and the read goes on: the memory does not depend on the size of the header, and what was read before the limit still counts. The result is the `gtf_header` fact (`gtf-header-1`), which only exists with `identify` and only when the GTF is structurally valid.

The `##description` line of GENCODE (`evidence-based annotation of the human genome (GRCh38), version 44 (Ensembl 110)`) gives `gencode`: `label` (`GENCODE 44`), `release` (`44`, or `M33` for the mouse), `organism`, `assembly` (absent when the line does not carry one), `ensembl` and the `date` of the header. A file with this line is `declared`, and the text says that a header can be edited. The header is then checked against the content, and any of these makes it `conflict`, with the reason naming each one:

- the sequence names follow the Ensembl convention (`1`, `X`, `MT`), or mix it with `chr`. GENCODE names start with `chr`;
- a `gene` line carries the attribute `gene_biotype`, which Ensembl writes and GENCODE writes as `gene_type`. Only `gene` lines are scanned, whole keys only: reading the attributes of every exon would nearly double the time of `identify`.

A GTF without the line is `not_identified`: that includes an Ensembl GTF (`#!genome-build`), whose release is not in the header, a header with `##provider: GENCODE` but no release, a header whose description comes after the limit and a description that comes after the first feature. Ensembl GTFs are recognised only by checksum, and no Ensembl checksum is in the table.

**FASTA.** For a FASTA, plain or inside gzip or BGZF, the same read that validates the structure produces the **Sequence Collection digest** of GA4GH Refget Sequence Collections v1.0.0, level 0:

- `names`: the first word of each header, in file order.
- `sequences`: `SQ.` followed by the refget `sha512t24u` of each sequence, after dropping every character that is not a letter and upper-casing the rest. Line width, case, line endings and the text after the name do not change it.
- `lengths`: the length of each normalised sequence. It is reported but, as the specification says, it does not enter the collection digest.
- Each attribute is digested as its canonical JSON array (RFC 8785) with `sha512t24u`; the collection digest is the `sha512t24u` of the canonical object `{"names": ..., "sequences": ...}`.

The result is the `seqcol` fact (`seqcol-1`), which only exists when the FASTA is structurally valid. A name that is not UTF-8, a name longer than 64 KiB, names that add up to more than 256 MiB or more than 1,048,576 sequences make the collection impossible to digest; the fact then carries only a `reason`. The caps bound the memory a hostile file can claim.

The digest is compared with the embedded table of known references (`identity.Known`). A digest enters that table only after it was checked against an official seqcol or refget server, and each entry records its source. The table is empty today: the official server does not label its collections with names, and no official list pairs a name with a digest. A FASTA that matches no collection and no checksum is `not_identified`, with the digest when there is one; that is not an error. The seqcol digest is always reported, also when the checksum is what identified the file.

Before any digest is trusted, `identity.Verify` checks the `sha512t24u` known vectors, like `checksum.Verify` does for the hashes, and the embedded checksum table. A file that changes during the read is `unreadable`, never identified.

`--json` writes its own document, with its own `schema_version` (today 1, since `identify` is not released yet and these fields were added before it was), independent of the one of `check`: `files[]` (`path`, `status`, `content`, `size`, `seqcol` with `digest`, `names_digest`, `lengths_digest`, `sequences_digest` and `sequences`, `reference` with `name`, `digest` and `source`, `checksum` with `name`, `file`, `md5`, `origin` and `retrieved`, `gencode` with `label`, `release`, `organism`, `assembly`, `ensembl` and `date`, `reason`) and `exit_code`. `status` is `identified`, `declared`, `conflict`, `not_identified`, `invalid` or `unreadable`. `identify` raises no assertion, so the `diagnoses/` catalogue has no entry for it: its `reason` is the sentence.

**Heavy door** — study with a database.

| Command         | Slice | Function                                                                                                                |
| --------------- | ----- | ----------------------------------------------------------------------------------------------------------------------- |
| `fetch`         | 3     | main flow: metadata, download, verification                                                                             |
| `verify`        | 3     | with a database: funnel of §7 and scrub (scrub in Slice 4)                                                              |
| `status`        | 3     | state of the study; `--require-complete` (Slice 5) exits 3 if incomplete                                                |
| `adopt`         | 4     | registers a pre-existing file under custody                                                                             |
| `manifest`      | 4     | exports a portable manifest                                                                                             |
| `attest`        | 4     | compares the collection against a manifest                                                                              |
| `recheck`       | 5     | `--upstream`: re-queries the source and compares with the stored one                                                    |
| `doctor`        | 7     | environment diagnosis                                                                                                   |
| `redact-report` | 7     | bug report safe to publish                                                                                              |
| `export`        | 7     | TSV/JSON/CSV with custody                                                                                               |
| `gc`            | —     | (postponed, `BACKLOG.md`) cleanup of orphan `.part` and expired `raw_responses`; `--dry-run` mandatory before `--force` |

**The CLI contains no logic.** It reads flags, resolves config, calls the library, translates an error into an exit code. Behaviour that exists only in the CLI is a bug.

---

## 17. Cross-cutting requirements

- Paths stored relative to the study root, never absolute.
- Sanitise a name coming from the source: reject traversal, NUL, separators, name > 255 bytes, and normalise unicode before comparing.
- `.part` and final on the same filesystem (atomic rename), verified by `doctor`.
- Pre-check space with a 5% margin; re-check every 1 GiB.
- Phase 1 validation (local, zero I/O) before any network call.
- Every timestamp in UTC, RFC 3339 format.
- `context.Context` is the first parameter of every I/O operation.
- `-race` on every concurrency test.
- No network access to a non-configured host. See `CONFIDENTIALITY.md` §5.11.
- Each file is read once per operation; every derivation from content goes through the single read of §7.
- Development and test platform: Linux or WSL. Native Windows supports only the light door until a Windows CI exists.

---

## 18. Decisions formerly open, now resolved

| Question                                  | Decision                                                                                                                                                                   |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Retention of `raw_responses`              | zstd, TTL 90d, `--keep-raw` turns off the TTL; restricted stores only the hash                                                                                             |
| `.part` when attempts are exhausted       | never deleted automatically; `gc --dry-run` before `--force`                                                                                                               |
| `experiments` ↔ reference                 | `experiment_references` table, not an FK; an experiment admits two builds                                                                                                  |
| Generation of `files.id`                  | `fetched`: `hex(BLAKE3(sample_id \|\| url \|\| supersedes))[:32]`. `adopted`: `hex(BLAKE3(sample_id \|\| path))[:32]`. Re-fetch and re-adoption idempotent by construction |
| Integrity structure                       | BLAKE3 tree with Bao outboard; CVs at the 8 MiB level in `file_groups`; grid of independent hashes discarded                                                               |
| CVs and outboard after final verification | all of them, forever; they enable reverification and attestation by slice                                                                                                  |
| Group size                                | fixed 8 MiB, power of 2 of 1 KiB chunks; not configurable in v1                                                                                                            |
| Structural revalidation                   | only when the extractor version changes; same hash implies same structure                                                                                                  |
| `stat` gate                               | decides what to read; never produces `verified`                                                                                                                            |
| Writer batch                              | 100 events or 50ms; `fatal` without batch                                                                                                                                  |
| Speculative prefetch                      | out of v1                                                                                                                                                                  |

### Open questions

| Question                                              | How to resolve                                                                     |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Source of Ensembl's official checksums for `identify` | GENCODE publishes an `MD5SUMS` per release and it is in the table. Ensembl's `CHECKSUMS` (a `sum` checksum, not MD5) per release still has to be surveyed before Ensembl GTFs can be identified. |

### Resolved in Slice 1

`lukechampine.com/blake3` exposes what the per-group tree needs: the `lukechampine.com/blake3/guts` subpackage offers `CompressBuffer`, `ParentNode` and `ChainingValue`, enough to compute the CV of each 8 MiB group during streaming and combine them up to the root. The outboard assembled by `facts` matches that of the `bao` package byte for byte (`TestOutboardMatchesBao`), so the alternative of per-group BLAKE3 as an independent hash was not needed.

---

## 19. Tests

See `TESTING.md`. Summary of the requirements that the SPEC imposes:

- Fake HTTP server per source, versioned fixtures, offline and deterministic.
- Fault injection: Range ignored, wrong Content-Length, truncated body, reset, 429 with Retry-After, 412, ENOSPC, failing fsync.
- Resume property test: kill at a random offset N times, the final checksum always matches.
- Tree property test: groups tile the file with no gap or overlap, and the root derived from the CVs is equal to the BLAKE3 of the whole file.
- Single-read test: bytes-read counter equal to the file size in `check` and in an uninterrupted download.
- Mutant suite (`TESTING.md` §7): every mutation of every class declared detectable is detected.
- State machine property test: only transitions of §4 persist.
- Fuzzing of each source's parsers.
- `sourcetest` conformance suite mandatory for every adapter.
- Leak sentinel in every artifact produced.
