# Colligo — Confidentiality

Normative document. It describes the ends through which restricted data can escape and the mechanism that closes each one.

The single principle of this document: **protection by type, not by discipline.** Every mechanism here is structural — the wrong path does not compile, does not open, or does not run. No mechanism depends on someone remembering to redact something.

**When it enters.** The types of §3 and the analyzer of §3.1 enter in Slice 0, because retrofitting types means rewriting. The rest of the regime — §5.2 to §5.12 and the table of §6 — enters in Slice 6, together with the first restricted-source adapter. Before that there is no restricted study to protect, and building the whole regime would delay the first useful delivery without protecting anything. See `DESIGN.md`, "Confidentiality enters through the types".

---

## 1. Threat model

The adversary is not malicious. It is the accident.

Cases this document prevents:

- A study `.db` with patient data synced to a personal cloud.
- A stack trace with a file path containing a medical record number, pasted into a public issue.
- An exported manifest that was thought to contain only hashes, containing hospital file names.
- A real test fixture committed by mistake.
- A dbGaP token in `argv`, visible to any user of the machine via `ps`.
- A credential in a versioned TOML.

Out of scope: an adversary with root access to the machine, an adversary with access to the database and the salt at the same time, and network traffic analysis. Colligo is not an encryption tool.

---

## 2. Classification

Every source has a declared classification. Two classes.

| Class        | Meaning                                                       |
| ------------ | ------------------------------------------------------------- |
| `public`     | Publicly accessible data. No retention or export restriction. |
| `restricted` | Controlled access: dbGaP, institutional data, patient data.   |

**The declaration is mandatory and fails closed.** A source configured without `classification` causes a load error naming the source. There is no default. There is no inference.

```toml
[sources.gencode]
classification = "public"

[sources.dbgap]
classification = "restricted"
```

A study that contains any `restricted` source is a **restricted study**, and restricted mode applies to the whole database — not per row. Reason: correlation. Knowing that patient X is in the same study as patient Y is already information.

The study's classification is written to `study_meta` at creation and is **immutable**. A public study cannot be promoted to restricted, because the data has already been written in the clear; the promotion would be a hack. The error instructs the user to create a new study.

**A derived artifact inherits the strictest classification among its parents.** A GTF merged from GENCODE (`public`) with a `restricted` file is `restricted`. Enforced at the registration of the derivative, not at use.

---

## 3. Types that do not leak

Three types in the core. No sensitive data travels as a bare `string` at any point of the system.

### `Secret`

Wraps a credential. It implements nothing that prints it.

```go
type Secret struct{ v string }

func (Secret) String() string          { return "[REDACTED]" }
func (Secret) GoString() string        { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (s Secret) reveal() string        { return s.v }
```

`reveal` is unexported. Only the transport package reaches the value, at the moment of building the header. `%v`, `%s`, `%+v`, `log`, `json.Marshal` and `spew` all produce `[REDACTED]` by construction.

### `Name`

Wraps a file name and path.

```go
type Name struct {
    raw        string
    opaque     string
    restricted bool
}

func (n Name) String() string
func (n Name) Raw(fs FSAccess) string
```

The classification travels **inside** the `Name`. A `Name` is built only by the study — which knows the classification and the salt — or by the light door, which has no study and always builds a public `Name`. There is no global state consulted in `String()`.

`String()` returns `raw` if `restricted` is false and `opaque` if it is true.

`Raw()` is the only door to the real value and requires an `FSAccess`: a token whose constructor is unexported and lives in the internal filesystem package. Export, log and manifest cannot obtain an `FSAccess`, therefore cannot call `Raw()` — the error is a compile error, not a runtime one. An earlier version of this section said that `Raw()` "returns an error in an export or log context"; that is not detectable at runtime without an explicit parameter, and was replaced by the token.

### `Endpoint`

Wraps a URL. `String()` in a restricted study returns `scheme://host/[REDACTED]`. The query string **never** appears, in any class, because the query is where a credential hides.

### Consequence

The logger does not accept `string` for path, URL or credential fields. The signature requires the types:

```go
log.Warn("download failed", logf.Name("file", f.Name), logf.Endpoint("url", f.URL))
```

Passing a raw `string` to one of those fields does not compile. This is what makes the mechanism structural.

### 3.1 What the compiler does not catch

The guarantee above only holds if there is no side door. `logf` has no generic `String` field, but nothing prevents `fmt.Sprintf("%s", raw)` followed by `logf.Text`, nor a `string` with a path crossing code that does not use `logf`. The closure is a custom `go/analysis` analyzer, running in CI:

- It forbids converting `secret.Name`, `secret.Endpoint` and `secret.Secret` to `string` outside `internal/fsaccess` and `transport/`.
- It forbids `string` fields and parameters whose name matches `(?i)(path|url|uri|file|name|token|secret|key)` in core packages, except where annotated by type.
- It fails the build, it does not emit a warning.

Type plus analyzer is the guarantee. Type alone is intent.

---

## 4. Opaque identifiers

A restricted study needs stable identifiers for auditing without exposing names.

- At study creation, a 32-byte **salt** is generated from `crypto/rand`, written to `study_meta`.
- `opaque(name) = hex(BLAKE3(salt || name))[:16]`.
- Stable within the study: the same file always has the same opaque id, so manifest diffs work.
- Not correlatable across studies: different salts.
- Not reversible without the salt.

**The salt never leaves the database.** It does not go to a manifest, export, log, bug report or configuration backup. Every export routine has a test that fails if the salt appears in the output.

---

## 5. The ends, one by one

### 5.1 Database

`raw_responses` is the largest surface: it stores API responses in full.

| Class        | Behaviour                                                               |
| ------------ | ----------------------------------------------------------------------- |
| `public`     | Body stored compressed (zstd), default TTL 90d.                         |
| `restricted` | **The body is not stored.** `body_hash`, size and timestamp are stored. |

Enforced in the `store` layer, consulting the study's classification. The caller has no parameter to turn it off. A malicious or buggy adapter cannot persist a restricted body because the decision is not its own.

The audit survives: hash and size prove that the response was the same between two runs, which is what the audit needs to answer.

### 5.2 File permissions

On opening a restricted study:

- A POSIX platform is mandatory: Linux, macOS or WSL. A restricted study on native Windows is refused — the permissions below have no direct equivalent, and OneDrive is the system's default synced folder.
- Database file: `0600`. Study directory: `0700`.
- Looser permission → **error**, not a warning. Explicit override `--allow-insecure-permissions`, which writes an event to `tool_runs`.
- WAL and SHM inherit the database's mode; checked after the first write, because SQLite creates them.
- Public study: warning only.

### 5.3 Sync directories

On opening and in `doctor`, the study path is compared against known cloud-sync patterns (`Dropbox`, `Google Drive`, `OneDrive`, `iCloud Drive`, `Sync`, `Nextcloud`, and the contents of `COLLIGO_SYNC_DIRS`).

Restricted study inside one of them → **error**. Override `--allow-synced-path`, recorded.

This is not paranoia: it is the most common vector of accidental leakage of research data.

### 5.4 Credentials

**A credential in TOML is a load error.** The parser rejects any key that matches `(?i)(password|passwd|secret|token|api_?key|credential|bearer)` under `[sources.*]`, naming the key and the line.

Allowed origins, in this order:

1. Environment variable, with the name declared: `credential_env = "COLLIGO_DBGAP_TOKEN"`.
2. External command: `credential_command = "pass show dbgap/token"`. Stdout captured as a `Secret`, stderr discarded, 10s timeout.
3. File pointed to by `credential_file`, which must be `0600`.

**Never in `argv`.** Colligo has no flag that accepts a credential. `ps` reveals nothing.

**Never in a query string.** The credential goes in a header. If a source only accepts a credential in the query, it is marked `credential_in_query = true`, and in that case the URL is never persisted or logged — the URL is stored with the query removed plus `url_query_stripped = 1`, and the query is reassembled from the config at request time.

### 5.5 Logs

- Path, URL and credential fields require the types of §3.
- Restricted study: the maximum log level is `info`. `debug` in a restricted study → error, because debug prints response bodies.
- No log writes a response body in a restricted study, at any level.

### 5.6 Errors and panics

- Typed errors carry `Name` and `Endpoint`, never `string`. A wrapped error prints redacted at any depth of `%w`.
- `recover` at the top of every worker: converts a panic into a typed error, records the type and location, **discards the original message** in a restricted study, because a panic message usually contains values.
- `GOTRACEBACK` is forced to `none` in a restricted study, so as not to dump variable values to the output.

### 5.7 Manifest

The manifest is made to circulate. It is the end of highest risk.

| Field              | `public` | `restricted` |
| ------------------ | -------- | ------------ |
| name / path        | real     | opaque id    |
| source URL         | real     | omitted      |
| size               | yes      | yes          |
| root and outboard  | yes      | yes          |
| source checksum    | yes      | yes          |
| format and custody | yes      | yes          |
| accession          | real     | opaque id    |
| salt               | never    | never        |

A restricted study's manifest carries `"classification": "restricted"` and `"redacted": true` in the header, so that the recipient knows the names are opaque.

A restricted manifest remains **fully functional** for attestation: `colligo attest` compares hashes, and hashes are not sensitive.

### 5.8 Tabular export

TSV/CSV/JSON of a restricted study uses opaque names under the same rules as the manifest. The `.custody.json` sidecar follows the same table.

Export of a restricted study to a path outside the study root → a warning with the destination path, and an error if the destination is a synced directory.

### 5.9 Repository

- `.gitignore`: `testdata/real/`, `*.local.toml`, `*.db`, `*.db-wal`, `*.db-shm`, `.colligo/`.
- Pre-commit hook with `gitleaks` on the diff.
- CI check: `git ls-files` matched against forbidden patterns; any tracked file under `testdata/real/` **fails the build**.
- Nothing institution-specific in code: host, project, internal field name and controlled-study identifier are configuration. See `CONTRIBUTING.md` §Adapter for a private source.

### 5.10 Tests

Two categories of fixture, never mixed. Detail in `TESTING.md` §2.

- `testdata/synthetic/` — versioned. The same **shape** as the real thing, invented values.
- `testdata/real/` — never versioned.

A test that depends on a real fixture sits behind the `restricted` build tag. Without the tag, it does not compile. With the tag and without the fixture, it **fails** — it does not skip silently, because skipping silently is the silent error this project exists to avoid.

A test failure involving a real fixture prints hash and offset, never content.

### 5.11 Network

Colligo has no telemetry, crash reporting, version check or analytics. This is a guarantee, not an intention, and it has a test:

The HTTP client's `DialContext` is instrumented in test to record every host contacted. The suite fails if the set of hosts is not a subset of the configured sources. No additional host, ever.

### 5.12 Bug report

`colligo redact-report` produces output that is safe to paste in public:

Includes — tool version, git SHA, Go, OS/arch, config hash, classification per source, effective PRAGMAs, file count per state, error types of the last 20 failures, result of `doctor`.

Excludes — names, paths, URLs, accessions, bodies, salt, credentials, and any value with entropy.

Stable format, so that two machines can be diffed.

---

## 6. Verifiable invariants

Every line of this table is a test. If any fails, the build fails.

| Invariant                                        | How it is tested                                       |
| ------------------------------------------------ | ------------------------------------------------------ |
| Salt does not appear in any output               | grep on the output of every export command             |
| Credential does not appear in any output         | high-entropy sentinel credential; global grep          |
| Restricted body does not reach the database      | inspection of `raw_responses` after a restricted fetch |
| `%v` of every domain type is safe                | reflection over the exported types; format and grep    |
| `Raw()` unreachable outside the filesystem       | analyzer §3.1 and a negative compile test              |
| No host other than the configured ones           | instrumented dialer                                    |
| Restricted database permission is `0600`         | stat after opening, including WAL and SHM              |
| Restricted manifest does not contain a real name | fixture manifest with a sentinel name; grep            |
| `debug` in a restricted study fails              | expected-error test                                    |
| Real fixture never tracked                       | `git ls-files` in CI                                   |
| Derivative of a restricted parent is restricted  | registration of a mixed derivative                     |

The sentinel technique deserves a note: a unique, improbable value (`COLLIGO_CANARY_7f3a…`) is injected as a file name, credential and response body in the test fixtures, and the suite greps for it in **every** artifact produced — log, export, manifest, report, database. A single leak at any end shows up.

---

## 7. What this document does not solve

Stated clearly so as not to create false confidence:

- **It does not encrypt anything at rest.** If the disk is not encrypted, the data is in the clear. Use the operating system's disk encryption.
- **It does not prevent deliberate exfiltration** by someone with access to the database and the salt.
- **It does not replace access approval.** Colligo does not know whether you have the right to the data.
- **It does not anonymise the content of the files.** A FASTQ is identifiable by nature; no opaque name changes that. Colligo protects metadata and the audit trail, not the primary data.
- **It does not protect against the downstream pipeline.** If your next tool writes paths to a log, the problem comes back.
