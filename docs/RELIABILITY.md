# Colligo — Reliability

Normative document. A catalogue of the silent failure modes and the defence against each.

A silent error is one that interrupts nothing, raises no warning, and produces a plausible result. It is the only class of failure that consumes months of work before being discovered — and sometimes it is discovered by a reviewer, not by you.

The document is organised by failure mode, not by component, because that is how the problem shows up in real life.

---

## 1. Posture: fail closed

`DESIGN.md` says "never silent about problems". The naive implementation of that is to warn and carry on, which in practice produces warnings that nobody reads.

Colligo fails closed. Three severities:

| Severity   | Effect                                                              |
| ---------- | ------------------------------------------------------------------- |
| `fatal`    | Aborts immediately. Partial state saved. Export impossible.         |
| `blocking` | Recorded. Other files carry on. The study is **not** left complete. |
| `advisory` | Recorded and printed. Does not affect completeness.                 |

**Strict mode is the default.** In it, any `blocking` still pending at the end of the run produces a non-zero exit code. `--permissive` leaves the exit code at zero (at 5 under `--quick`, see below), but **hides nothing**: the export still marks the study as incomplete and each row still carries its custody state. Permissive changes whether the pipeline stops, never whether the information exists.

### Exit codes

| Code | Meaning                                                                   |
| ---- | ------------------------------------------------------------------------- |
| 0    | Complete and verified                                                     |
| 1    | Usage or configuration error (nothing was attempted)                      |
| 2    | `fatal`: integrity, identity, authentication, disk                        |
| 3    | Incomplete: `blocking` pending                                            |
| 4    | Interrupted: cancelled, state saved, resumable                            |
| 5    | `check --quick`: nothing found in what was read, content not read in full |

**Reach of `fatal`.** A failure of the tool itself (known hash vectors, canary) aborts the whole run: nothing else is trustworthy. A `fatal` failure of one file (unreadable, HTML page, checksum that does not match) ends the verification of *that* file; on the light door the others carry on being verified, because there is no state to save and stopping would hide their diagnosis. The exit code is that of the worst severity found.

`--expected` with the same key repeated (case-insensitive, even with the same value) is a usage error, code 1, before any file is read: two values for the same key are ambiguous and letting the last one win would hide a copy error.

`--expected` with a malformed hash is also a usage error, code 1, before any file is read: MD5 has 32 hexadecimal characters, SHA-256 and BLAKE3 have 64, case-insensitive. A malformed expected value is the typist's error, not evidence of a corrupted file, so it never becomes a fatal `checksum_mismatch`.

`--cross-check` writes `colligo.sha256` and `colligo.b3` in the GNU format: if the name has `\` or a line break, the line starts with `\` and the name comes out with `\\` and `\n`, which `sha256sum -c` and `b3sum -c` understand. A symbolic link to a directory is never followed (avoids cycles with `--recursive`); it comes out as "symbolic link to a directory, not followed" and in `skipped_directory_links` in the JSON, and not as an unrecognised file.

The `5` exists because `colligo check --quick x && pipeline` must not proceed as if the file had been verified: a gzip truncated in the middle passes the quick check. The `0` stays reserved for "complete and verified". A problem found by the quick check still exits `2` or `3`; `--permissive` lowers a `blocking` to `5`, never to `0`.

Codes 3 and 4 are distinct on purpose. `3` means "finished and is wrong". `4` means "did not finish". A script that treats the two the same loses the difference that matters.

---

## 2. Catalogue of failure modes

### M1 — Intact file, but the wrong one

The GTFs of Ensembl 110 and 111 are both valid and pass any checksum. They produce different results. No byte verification detects it.

**Defence.** A reference file has identity beyond the hash: `assembly`, `release`, `source`, `asset_kind`. The pipeline declares what it expects; a divergence is `fatal`, not a warning.

```toml
[reference]
assembly = "GRCh38"
release  = "GENCODE 44"
```

A reference file acquired without a determinable `release` gets `confidence = inferred` and is `blocking`. A reference without provenance is useless for reproducibility — this is exactly the problem that the reference-provenance literature documents.

The declaration is confirmed by the content whenever possible (`colligo identify`): Sequence Collection digest for FASTA, `##description` header for GENCODE GTF, hash against official checksums for Ensembl. Confirmed by the content → `confidence = confirmed`. Content that contradicts the declaration → `fatal`.

### M2 — The source swapped the file

Same URL, different content. GEO re-uploads supplementary files; the Ensembl FTP reorganises releases.

**Defence.** Capture of `ETag` and `Last-Modified` on the first download, stored in `files`. On every revisit:

- `If-Range` on resume, to detect a swap in the middle.
- Different content at the same URL **never overwrites** a `done` file. A new row is created with `supersedes` pointing to the previous one, both preserved, and the divergence is `blocking`.

Silent replacement is impossible by construction: there is no code path that does a destructive UPDATE on a `done` file.

**A file that changes during the read.** Same path, different content, but right under the read itself: a download still in progress, a pipeline stage rewriting the file. `CheckFile` does an `fstat` on the open descriptor before and after the read and compares size, mtime and ctime (with nanoseconds) and inode. Any difference produces `changed_during_read`, `blocking`, mode M2: the hash may describe bytes that never existed together, so the file does not come out `verified`, receives no hashes in the report or certificate, and the remaining assertions are `skipped` because of it. After the read `CheckFile` also does a `stat` by path (a function of `internal/fsaccess`) and compares inode and device with those of the open file: a path that points to another file (`mv new.fastq.gz reads.fastq.gz`, which is how safe downloads end) gives `Observed: replaced`, and a path that disappeared gives `removed`. Outside Linux only size and mtime are compared, and the swap by path is not seen. An in-place overwrite that preserves size, mtime and ctime (a stopped clock, `utimensat` after the write) is not seen.

### M3 — Error page saved as data

The server returns `200` with an HTML error. You are left with a 4 KB `.fastq.gz` that is a web page.

**Defence.** Check of the magic bytes in the first group, before writing the second. If the declared format is gzip, the first two bytes are `1f 8b`. `fatal` for the file, and the `.part` is preserved for inspection.

It costs microseconds and catches a disproportionate number of real failures.

**The wrong file, by name.** The same first bytes name the files people hand to a check by mistake: `NCBI.sra` (`input_sra`), a BGZF whose content starts with `BAM\1` or a `CRAM` file (`input_alignment`) and a ZIP archive (`input_archive`). Each is `blocking`, like the generic `format_unknown` it replaces, so none of them is ever `verified`; the report says what the file is and what to do. Detection is by content: a BAM named `reads.fastq.gz` is still a BAM.

### M4 — Truncation that slips through

The worst mode. A cut `.gz` decompresses partially. Many downstream tools accept it without complaint and produce plausible numbers.

**Defence.** Structural validation of the format at the end of every download, **after** the checksum and **before** the rename to the final name. The goal is to verify that the file ends where it should end.

Cost, stated precisely: for BGZF, BAM and CRAM there is an end marker, and the verification is cheap. For plain gzip and FASTQ there is none — ISIZE only checks by decompressing everything, and counting lines too. In these formats the validation is a full read of the decompressed content. That is why it happens **once per hash root and validator version** (`DESIGN.md`, "Facts and assertions"), and the same read extracts the counts and IDs used by M5 and M15.

| Format      | Prefix          | End-of-file verification                                                                                                                    |
| ----------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| gzip        | `1f 8b`         | last member closes; ISIZE matches the decompressed size mod 2^32                                                                            |
| bgzf        | `1f 8b` + XLEN  | contiguous BSIZE chain up to the end; EOF block present                                                                                     |
| bam         | `BAM\1` in bgzf | bgzf EOF block present                                                                                                                      |
| cram        | `CRAM`          | EOF container present                                                                                                                       |
| fastq / .gz | `@`             | line count a multiple of 4; last line complete; blank line only after the last record, and then `advisory`; UTF-8 BOM at the start rejected |
| fasta       | `>`             | ends with a newline; no truncated sequence line; `;` comment line rejected; UTF-8 BOM at the start rejected                                 |
| vcf / .gz   | `##fileformat`  | header present; last line has a valid field count                                                                                           |
| gtf / gff3  | text            | every feature line has 9 tab-separated fields; UTF-8 BOM at the start rejected                                                              |
| hdf5 / h5ad | `\x89HDF\r\n`   | superblock intact; root opens                                                                                                               |
| pdb         | text            | `END` record present                                                                                                                        |
| mmcif       | `data_`         | last block closes                                                                                                                           |
| rds         | RDS magic       | prefix only                                                                                                                                 |
| tsv / csv   | —               | consistent field count; last line complete                                                                                                  |

The declared format comes from the profile or the TOML. Inferred from the extension → `confidence = inferred` and `advisory`.

Structural validation failure → status `invalid`, severity `blocking`. The file never reaches `done`, therefore never counts toward completeness, therefore never leaks into the pipeline as if it were good.

**If I had to choose a single defence in this document, it would be this one.**

### M5 — Misaligned R1/R2 pair

Different read counts, or files of different samples paired by mistake. Produces garbage alignment with no error at all.

**Defence.** When both members of a `pair_id` reach validated:

- Identical record count.
- ID of the first and last record compatible after discounting the pair suffix (`/1`, `/2`, ` 1:`, ` 2:`, and the `.1`/`.2` of `fastq-dump --readids`, only in the form `<accession>.<spot>.<1|2>`, such as `SRR001666.1.1`). A trailing `.1` or `.2` outside that form is part of the ID: `SRR1.1` and `SRR1.2` are different spots.

Divergence → `blocking` on both files, with `pair_count` (different counts) or `pair_ids` (different first or last ID). Neither becomes `done`.

**How pairs are formed.** By name, in the same directory: same stem, `_1`/`_2`, `_R1`/`_R2` or `.1`/`.2`, and the same chunk number (`_001`). `_R1_001` with `_R2_002` do not pair. If two files compete for the same mate (for example `s_R1.fastq` and `s_R1.fastq.gz`), none of the three receives a pair assertion. A mate in gzip and the other in plain text pair normally.

**Missing mate.** A file with a mate suffix (`_R1`, `_2`, `.1`...) whose other mate is not among the files verified receives `pair_missing`, `advisory`, mode M5, with the name of the expected mate. It only fires when the run has more than one file. It also applies to mates in different directories and to `_R1_001` with `_R2_002`, which do not pair.

**In a samplesheet.** `check --samplesheet` applies the same invariant to the mates the samplesheet declares, ignoring the names. The failed assertion carries the column and the line, and when two lines exchanged their `fastq_2` files, `sheet_mates_swapped` names both lines. A row without `fastq_2` is single-end and gets no `pair_missing`.

**What the invariant does not catch.** It compares the count and the first and last ID. Two files with the same number of reads, the same first and the same last ID, but swapped or reordered reads in the middle, pass as `verified`. Checking all the IDs would require storing or summarising the sequence of IDs in the same read; that is outside the current slice.

### M6 — Partial success with exit zero

47 of 50 files downloaded. The pipeline carries on with 47. The result comes out. Nobody notices.

**Defence.** Completeness is a first-class operation, not a manual inspection.

- In `fetch`, `expected_file_count` and `expected_set_hash` are stored (hash of the sorted set of file identities returned by the source).
- A later fetch that produces a different set → `blocking` "source set changed", with the diff in `discrepancies`.
- Completeness = every file of the study in `done` **and** a count equal to `expected_file_count`.
- With `[results]` declared and Scribo in the same database, completeness goes up to "processed": every expected result present and not stale (`CONTRACT.md` §7).
- `colligo status --require-complete` exits with 3 if not complete. It is the line that goes at the top of your analysis script.
- Export of an incomplete study **refuses** without `--permissive`.

### M7 — Interrupted run, forgotten

Ctrl+C, three days pass, you come back and forgot that partial state was left.

**Defence.** `tool_runs` records start, end and exit code. A run without an end record is an interrupted one. On opening the database, **without being asked**, Colligo reports: when it was, how many files were left outside `done`, and the exact command to resume.

### M8 — Config changed between runs

Half the collection acquired with one set of parameters, half with another. Silent and insidious.

**Defence.** `tool_runs.config_hash` — canonical hash of the effective configuration, after precedence is resolved. A change of hash for the same study → `advisory` on opening, listing which keys changed. A change in a key that affects integrity (group size, extractor version, checksum policy) → `blocking`.

### M9 — Bug in the tool itself

If the hash computation has an error, everything "verifies" and everything is wrong. No internal defence reaches that, because the defence uses the faulty code.

**Layered defence.**

1. **Known vectors at start-up.** Official vectors of BLAKE3, MD5 and SHA-256 run on every database opening. Failure → `fatal`, tool or environment suspect. Costs microseconds.
2. **Canary.** Each study has a 128 KiB canary file of deterministic content and known hash, verified on every run through the same code path as the real files. If the canary fails, the problem is the environment or the tool, not the data — and the message says so, so that you do not go hunting for corruption where there is none.
3. **External cross-verification.** `colligo check --cross-check` emits files in the format of `sha256sum` and `b3sum`, for you to check with a tool that is not ours. In the test suite, when `b3sum` is on the PATH, the hashes are compared against it.
4. **Independent certificate verifier.** `tools/verify_cert.py` recomputes hashes and counts without Colligo code and checks the certificate (SPEC §13). It runs in the suite over the whole synthetic corpus.
5. **Second implementation of the extractor, when it exists.** If the Rust accelerator of `DESIGN.md` enters, its facts and those of the Go extractor are compared byte for byte in the suite, and `check --cross-extract` runs both on the same file in production. Divergence → `fatal` with both values, and the message points at the tool, not the data.

Two independent implementations of the critical check is the only real defence against a bug in the check.

### M10 — Disk space exhausted midway

`statfs` before starting is not enough: another process can consume the disk during a transfer of hours.

**Defence.** Pre-check with a 5% margin beyond the expected size. `ENOSPC` during a write is `fatal`, but the error path **writes the safe offset before aborting** — that is what makes "partial data has value" true under the failure that most threatens partial data. Space re-check every 1 GiB transferred, with a warning at 95% usage.

### M11 — Divergent chromosome convention

UCSC/GENCODE's `chr1` against Ensembl's `1`. If the conventions do not match between genome and annotation, quantification fails silently: no error, only wrong numbers.

**Defence.** `chromosome_naming` is first-class metadata of every reference asset, detected by sampling the file. Registration of a derived artifact whose parents diverge in convention → `fatal`. Colligo **never converts automatically**; converting silently is creating the error one is trying to avoid.

Generalisation: `colligo compat` compares the coordinate system of heterogeneous files by the `sorted_name_length_pairs` digest of the Sequence Collections standard, and returns the name diff when they do not match. Name convention is the visible case; a divergent length with the same name — a different build — is the invisible case that the digest also catches.

### M12 — Derived artifact without provenance

A GTF merged from GENCODE with Dfam was not downloaded, it was built. If you do not know which version of each parent went in, the result is not reproducible — and the absence is invisible.

**Defence.** `derived_files` and `derived_parents`. Every derivative records parents, builder tool, its version and the hash of the parameters. A derivative without a registered parent is `blocking`. The manifest exports the complete tree.

### M13 — Wrong clock

Timestamps are the backbone of the audit. A wrong clock corrupts the order of events without corrupting a single byte.

**Defence.** Every timestamp in UTC. On opening, if the current time is earlier than the last recorded event, `blocking` with the difference. Without NTP, Colligo at least detects the inconsistency.

### M14 — Rotten fixture

The source changed the response format; your suite stays green because it tests against the capture from two years ago.

**Defence.** A weekly drift job, separate from the PR CI, that repeats the request of every recorded fixture against the real API and opens an issue on divergence; it never rewrites a fixture. It compares the structure Colligo reads and the values it uses, so a run of a fixture that the archive suppressed, or whose counts or MD5 changed, is caught as well as a renamed column. A source that does not answer is a failure of the job, never drift, and a moved or extra column is only a note: both would otherwise be alarms that teach people to ignore the issue. Detail in `TESTING.md` §3.

### M15 — Valid content, but smaller than the source declared

Documented case: an extraction with `fastq-dump` produced a read 2 with 1 bp instead of 98, finished with code 0, and the resulting FASTQ is structurally valid. There is no source MD5 for a locally extracted file, the structure passes, and the error only appears in trimming or alignment — when it appears.

**Defence.** ENA publishes `read_count` and `base_count` per run. The read that validates the FASTQ structure already decompresses everything, so it counts reads and bases at no additional cost. The observed count is compared against the declared one; divergence → `blocking`, with both numbers in `discrepancies`.

The two declared numbers are not equally reliable. Measured on 27 real Illumina runs from DDBJ, NCBI and ENA submissions, 52 files (2026-10-30):

- `base_count` equals the sum of the bases of every file of the run in 27 of 27 runs, whatever the label and the number of files.
- `read_count` has no single meaning, and the `library_layout` label does not predict which one holds:

| Run | Label | Files | `read_count` | Reads per file | It is |
|---|---|---|---|---|---|
| DRR021374 | PAIRED | 2 | 13767 | 13767, 13767 | each file |
| ERR12477684 | PAIRED | 2 | 3524 | 1762, 1762 | sum of the files |
| ERR5060520 | SINGLE | 2 | 8014 | 4007, 4007 | sum of the files |
| ERR5514981 | SINGLE | 3 | 4923 | 7, 2458, 2458 | sum of the files |
| SRR10663037 | PAIRED | 3 | 12859 | 1, 12858, 12858 | the pair count plus the unpaired file |

So the rule is not "R1 plus R2 equals `read_count`": that raises a false `blocking` on ordinary paired runs, and a check that cries wolf teaches users to ignore it. Nor does the label choose the reading. The rule is:

1. `base_count` is the blocking comparison, against the bases of every file listed for the run. Divergence → `blocking` `count_mismatch`. This is what catches the shortened read 2.
2. `read_count` is checked against the three readings seen above: each file, the sum of the files, the pair count plus the unpaired files. A match passes. When `base_count` holds and no reading matches, the content agrees with the declared bases and the MD5 and only the metadata field is unexplained: `advisory`, never `blocking`. When `base_count` fails, the run is `blocking` whatever `read_count` says.
3. `layout_mismatch`, `advisory`, when the label says single and the files form a pair (`pair_count` and `pair_ids`): the data is intact, the label is wrong, and a pipeline that trusts the label would treat the mates as two samples. A label that says paired for one file is not judged, because Colligo does not check for interleaved files. In the table, ERR5060520 and ERR5514981 (whose `_1` and `_2` are a pair next to an unpaired file) both get it.

`colligo check --source ena:<accession>` applies this rule at the light door (SPEC §16). The bases are compared once per run, when every file of the run was read; a file of a run that fails gets `source_run_failed`, so it never reads `verified`. A count the source left empty is reported as not declared and skipped, never compared as zero.

A renamed download is not a false alarm: the local file is linked to the declared one by its computed MD5 and size when no name matches, and the report says it was linked by content (`linked_by: "md5"`). The link comes after the name and never replaces it, and two candidates are `source_file_ambiguous`, not a guess. What it cannot link is a file that was both renamed and decompressed, whose name and MD5 both fail to match: it stays `source_file_missing` (blocking) for the declared file and `source_file_unlisted` for the local one. That is an incomplete-looking run for intact data, the price of not trusting a name or a hash that the content cannot support; the user restores the name or checks the compressed original.

Trying several readings of `read_count` is safe only because it is not the blocking comparison: with `base_count` independent and blocking, a coincidental match cannot let a shortened run through.

The sample is small: Illumina only, the smallest runs of each label and file count, no long reads. The adapter records every run whose `read_count` no reading explains as a new fixture instead of widening the rule silently.

Regression fixtures are the recorded metadata responses of the runs above, not the files.

It is the cheapest defence per real failure caught in this catalogue: zero extra reading, one metadata query that fetch already makes.

### M16 — The source condemned the data after you downloaded it

A run can be suppressed at ENA after the download. Suppressed disappears from the search, but remains retrievable by accession — the pipeline keeps working. Permanent suppression is for data considered incorrect with no prospect of correction. Nothing on the disk changes, so no local verification detects it.

**Defence.** `colligo recheck --upstream` re-runs the metadata part of the adapters and compares with `raw_responses`: record status, set of files, published checksums. Suppressed run → `blocking`. Published checksum different from the stored one → `blocking`, with the same `supersedes` logic as M2. A study with a `recheck` older than `recheck_window` → `advisory` on opening.

At the light door, `colligo check --source` applies the status part at the moment of the check: a run whose status is not `public` is `source_status`, `blocking`, including a run that lists no files.


### M17 — Single-cell chemistry declared wrong

Declaring 10x v3 for v2 data, or the other way round, is a common metadata error. The pipeline runs, the wrong barcode whitelist drops the valid-cell rate, and the result comes out plausible and worse.

**Defence.** `barcode` fact: R1 length (26 bp in 10x v2, 28 bp in v3) and the hit rate of a sample of barcodes against the known whitelists. Observed chemistry different from the one declared in the metadata or the TOML → `blocking`. Cell Ranger detects the chemistry by itself, but inside the pipeline; here the divergence appears earlier, against what GEO declared.

### M18 — GEO matrix incoherent with GEO itself

A supplementary count file with swapped columns, missing samples, column names that do not correspond to the GSMs, or gene IDs from a namespace incompatible with the declared genome. The matrix is a valid TSV; nothing breaks.

**Defence.** `matrix` fact: column names, row count and ID namespace (Ensembl with version, Ensembl without version, symbol). Assertions: columns ⊆ the study's samples and covering all of them; namespace compatible with the declared reference. Column divergence → `blocking`; unexpected namespace → `advisory` with the observed namespace.

### M19 — The same data deposited twice

Two runs with identical content, or read from the same flowcell and lane, enter as distinct replicates in a meta-analysis and inflate the evidence.

**Defence.** Facts are indexed by the hash root, so identical content across accessions shows up at no cost: `advisory` listing the accessions. The `read_header` fact (instrument, run, flowcell, lane) points to the subtler suspicion — different files from the same lane declared as different samples — also as `advisory`, because there are legitimate designs that produce this.

**In a samplesheet.** The same root listed twice is `blocking`, not `advisory`, because a pipeline reads the samplesheet literally: `sheet_duplicate_row` for one sample listed twice with the same files, `sheet_shared_file` for the same content under two samples or as both mates of one line. Lines of one sample with different files are lanes and are not flagged.

### M20 — A crash in the middle of a write leaves a state that lies

Power goes out, the job is killed by the scheduler, the disk vanishes. If the order of the writes is wrong, the resumption finds a file with a final name that was never verified, a group marked `verified` whose content did not reach the disk, or an event in the database without the corresponding file.

**Two-layer defence.**

1. **Formal model.** The resume protocol, the rename and the state machine are specified in TLA+ and verified by model checking before implementation: in no sequence of failures and restarts does a file reach `done` without checked content, and no group is left `verified` without an `fsync` of the content it covers.
2. **Crash at any point.** The suite enumerates each write, `fsync` and `rename` of a `get` and of a `fetch`, kills the process at each one, resumes, and checks the invariants of the model. `TESTING.md` §6.1.

Mandatory order, derived from the model: write bytes → `fsync` of the `.part` → mark group `verified` → commit the database. Rename of the `.part` only after `fsync` and all the assertions; `fsync` of the directory after the rename.

---

## 3. Visible custody

Per-file custody state, always available and always exported:

| State        | Meaning                                                                                      |
| ------------ | -------------------------------------------------------------------------------------------- |
| `verified`   | Content read and checked, within the scrub window. The `stat` gate never produces this state |
| `stale`      | Was checked before; window expired or baseline changed                                       |
| `unverified` | Never checked by content                                                                     |
| `invalid`    | Check failed; diverging groups or failed assertion known                                     |
| `adopted`    | Under custody, with no origin checksum to cross against                                      |
| `suspect`    | Added to any state: Scribo observed, after the pipeline, an incoherent result for this input |

Every export carries `custody_state`, `last_verified_at` and `verified_fraction` per row, plus a `.custody.json` sidecar with the study summary. Whoever consumes the TSV downstream sees the state in the file they are already reading, without needing to query the database.

`adopted` deserves a note: an adopted file that came without a published checksum has **internal** integrity guaranteed from the moment of adoption, but Colligo cannot claim that it arrived correct. The distinction is recorded instead of being dissolved.

---

## 4. `colligo doctor`

Environment diagnosis. Runs at any time, and automatically before a long transfer.

- Free space against the queue's expected size
- Permissions of database, WAL, SHM and directory
- Path inside a cloud-sync directory
- SQLite version and effective PRAGMAs (reads back, does not assume the SET worked)
- Known hash vectors
- Study canary
- Clock sanity
- Presence of a credential per configured source — **presence only**, never the value
- Previous interrupted run
- `.part` and final file on the same filesystem
- Connectivity and effective limit per source

Readable output and `--json`. Zero identifiers, so that it can be pasted in public.

---

## 5. Precedence of the defences

When defences disagree, this is the order. It exists so that there is no doubt in the implementation.

1. Known hash vectors — if they fail, nothing else is trustworthy
2. Study canary
3. Clock sanity
4. Permissions and classification
5. File identity (assembly, release, ETag, file stable during the read)
6. Source checksum
7. BLAKE3 tree
8. Structural format validation
9. Pair invariants
10. Count declared by the source
11. Declared chemistry and matrix coherence
12. Completeness of the set
13. Record status at the source
