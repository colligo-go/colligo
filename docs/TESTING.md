# Colligo — Testing

Normative document. What guarantees that a contributor's hidden bug does not reach the user.

Premise: human review does not scale and is not reliable for API parsing logic that nobody but the author knows. What scales is a suite that the contributor's code must **pass**, not one that they must write.

---

## 1. Adapter conformance

The core publishes `sourcetest`. The contributor does not write the invariant tests.

```go
func TestGENCODE(t *testing.T) {
    sourcetest.Run(t, sourcetest.Config{
        New:       func() colligo.Source { return gencode.New() },
        Fixtures:  "testdata/synthetic",
        Accession: "GENCODE_44_GRCh38",
    })
}
```

What `sourcetest.Run` asserts, without the contributor being able to turn it off:

| Invariant                      | Verification                                                              |
| ------------------------------ | ------------------------------------------------------------------------- |
| Cancellation respected         | ctx cancelled before the call → `ctx.Err()`, zero effects                 |
| Determinism                    | same fixture 2× → byte-identical output after serialisation               |
| No global HTTP client          | injected `RoundTripper`; real dial → `t.Fatal`                            |
| Rate limit honoured            | fake clock; N calls with a 1/s limit take ≥ N−1s                          |
| Valid roles                    | every `Sample.Role` ∈ `Profile.ValidRoles()`                              |
| Safe name                      | hostile fixtures (see §2) → rejected or sanitised under the root          |
| Correct typed errors           | 404/401/429/503/412 → `errors.As` matches; 429 honours `Retry-After`      |
| Raw persisted before the parse | a parse that fails still leaves an audit trail                            |
| No goroutine leak              | `goleak` at the end of each subtest                                       |
| No disk write                  | instrumented FS; any `open` for writing → `t.Fatal`                       |
| No access to `Store`           | guaranteed by the signature; the adapter does not receive a `Store`       |
| Sentinel does not leak         | sentinel value in the fixtures; grep over the whole adapter output        |
| Declared format is known       | `Artifact.Format` ∈ the `format/` registry                                |
| Declared counts coherent       | `ReadCount`, `BaseCount` ≥ 0; `BaseCount` ≥ `ReadCount` when both present |
| Metadata without download      | `Fetch` and `Resolve` never request the body of a data file               |

`storetest` and `profiletest` follow the same pattern. `storetest` is a prerequisite for any alternative implementation of `Store`.

**`sourcetest` is public API.** Adding an assertion breaks existing adapters, therefore it is a semver event, decided consciously.

### Hostile fixtures, always included

Every adapter is tested against responses containing:

```
../../etc/passwd
..\..\windows\system32
name\x00null
name/with/separator
<255+ bytes>
аrquivo.fastq.gz          (Cyrillic а, homoglyph)
CON, NUL, PRN             (Windows reserved names)
file .fastq.gz            (leading and trailing space)
-rf                       (looks like a flag)
```

None of these may produce a write outside the study root, and none may make the adapter panic.

---

## 2. Fixtures

Two categories, never mixed.

### `testdata/synthetic/` — versioned

The same **shape** as the real response, invented values. It is what runs in CI and for any contributor. It contains the leak sentinels.

For the format validators, the synthetic corpus is produced by a versioned generator, `internal/corpus`: `go test ./internal/corpus -record` writes the files and a sibling `.meta.json` with generator, size, SHA-256, recording date and what the generator knows it wrote — count of records and bases, and which assertions must fail. Without the flag, the test fails if a file diverges from the generator or if a file the generator does not produce shows up. The expected never comes from a parser: `check` over the directory and the Python verifier are checked against it. The generator compresses with `klauspost/compress`, pinned in `go.sum`: the output of the stdlib's `compress/gzip` changes between Go versions, and the corpus needs the same bytes under any toolchain.

To re-record the corpus after changing the generator or the version of an extractor:

```
go test ./internal/corpus -record
git diff --stat testdata/synthetic
```

Review the diff before committing: a file that changed without the generator having changed is a sign of a bug.

### `testdata/real/` — never versioned

Real captures. It stays on the machine of whoever has access. Blocked by `.gitignore` and by a CI check that fails if any file under that path is tracked.

### Recording rules

- A fixture is **recorded, never written by hand**: `go test -record` hits the real API, writes the file and removes credentials.
- Each fixture has a sibling `.meta.json`: URL, capture date, tool version, hash. Without that, in two years nobody knows whether the fixture reflects reality.
- A fixture is never edited after being recorded. Mutation for testing happens **in the test code**, so that the diff is visible in review.

### Tests that depend on a real fixture

Behind the `restricted` build tag. Without the tag, they do not compile.

With the tag and without the fixture present: **`t.Fatal`, not `t.Skip`.** Skipping silently is exactly the silent error this project exists to avoid — a badly configured CI would pass green without running anything.

A failure involving a real fixture prints hash and offset. Never content.

---

## 3. Drift

The PR CI is 100% offline and deterministic. That is incompatible with detecting API change — unless the jobs are separated.

**Separate weekly job:** re-records all fixtures against the real APIs, compares with the versioned ones, and opens an issue on divergence with the structural diff (fields that appeared, disappeared or changed type), never with values.

For restricted sources, the job runs on the institution's infrastructure and never publishes the diff.

---

## 4. Fuzzing

A contract test catches what you foresaw. Fuzzing catches the rest. One target per source parser, plus one per format validator.

```go
func FuzzParseGEO(f *testing.F) { ... }
func FuzzValidateGzip(f *testing.F) { ... }
```

Seed with the synthetic fixtures. Invariants:

- Never panics
- Never returns a role outside `ValidRoles()`
- Never produces a path that escapes the study root
- Allocation does not grow disproportionately to the input
- Never returns a format outside the registry

Every PR runs each target for 60 s in the `fuzz` job of `ci.yml`, parallel to `test` and informative: it is not in `.github/rulesets/main.json`, because fuzzing is random and could block a PR over an old bug. The weekly job (`weekly.yml`) runs each target for 30 min, one job per target, restoring and saving the fuzz corpus with `actions/cache` between weeks. A target that finds a failure uploads the input as an artifact.

Every case fuzzing finds becomes a file in `testdata/fuzz/<Target>/` inside the package that owns the target (for example `format/testdata/fuzz/FuzzValidateFastq/`), committed with the fix. That is the directory `go test` reads on its own, so the required `test` check replays the case on every run, deterministically.

This is the item of highest value per line written against a bug that nobody imagined.

---

## 5. Property tests

**Resume.** Kill the download at a random offset N times; the final checksum always matches. It is the central test of the project.

**BLAKE3 tree.** For a random file size: 8 MiB groups with the sum of sizes equal to the total, contiguous offsets, no gap or overlap, and the root derived from the CVs equal to the BLAKE3 of the whole file, checked against `b3sum`. The last group, and the one-group file, are where the off-by-one lives.

**Single read.** An instrumented reader counts bytes read. In `check` and in an uninterrupted download, bytes read = file size. Any re-read fails the test.

**Facts are pure functions.** The same content with different names, paths and mtimes → the same facts, byte for byte. Changing an extractor's version invalidates only the facts of that `kind`.

**State machine.** Random sequences of events; only transitions of the table in SPEC §4 persist. The test reads the transition map — the test *is* the spec, not a copy of it.

**Equivalent extractors.** When an external extractor exists, for every file of the synthetic and fuzz corpus, the facts of the Go extractor and of the external one with the same `extractor_version` are identical byte for byte. A divergence automatically reduces the file to the smallest prefix that still diverges, so that the report points at the culprit record.

**Redaction.** For every exported domain type, format with `%v`, `%s`, `%+v`, `%#v` and `json.Marshal`, and assert that the sentinel does not appear. By reflection over the package, so that a new type enters the test automatically.

**Idempotence of `files.id`.** Fetch the same fixture twice → the same id, no duplicate row.

**Manifest and attestation.** A manifest exported and re-attested against its own collection always matches. Corrupt a known group → the attestation points at exactly that index.

---

## 6. Hostile filesystem and environment

FS wrapper injecting:

- `ENOSPC` on write
- `EIO` on read
- short write
- `fsync` that returns an error
- `rename` that fails
- mtime that goes backwards
- inode that changes between stats

This is where "partial data has value" is proven or revealed to be a slogan. The critical case: `ENOSPC` must write the safe offset before propagating.

Environment: clock going backwards, loose permission on the database, a PRAGMA that does not take effect, `.part` and final on different filesystems.

---

## 6.1 Crash at any point

The FS wrapper numbers each write, `fsync`, `rename` and database commit of a reference run of `get` and of `fetch`. For each number N, the suite runs again, kills the process with `SIGKILL` exactly at operation N, discards what did not receive `fsync` (simulating loss of the operating system's cache), resumes, and checks:

- No file with a final name without all assertions passed.
- No `verified` group whose content on disk does not match the CV.
- No event in the database pointing to a state that the disk does not support.
- The final file, when finished, is identical to that of the run without failure.

High cost: runs in the full weekly job and on a random sample of points in every PR.

## 6.2 Formal model

`spec/resume.tla` specifies the resume protocol, rename and state machine. The TLC model checker runs in CI with small parameters (3 groups, 2 failures, 2 restarts) on every PR that touches `downloader/`, `store/` or `verifier/`. The invariants of the model are the same as those of §6.1: the model proves it for the protocol, the crash-testing checks that the code follows the protocol.

## 6.3 Certificate and independent verifier

For every file of the synthetic corpus and of the mutants, `check --certificate` and `tools/verify_cert.py` are executed; the verifier must agree with each hash and count of the certificate, and reject every tampered certificate, including the `passed` of the assertions of an invalid file.

## 6.4 Adaptive concurrency

A fake server with configurable capacity, latency and 429 above a connection limit. Adaptive concurrency must converge to near the capacity, never exceed the configured ceiling, honour `Retry-After` and finish without a permanent error. Fake, deterministic clock.

---

## 7. Mutants: benchmark of silent failures

Versioned in the repository, reproducible by anyone. It is not optimisation — it is the evidence that "robust" is a measurement, not an adjective. It is also the project's publishable asset: no benchmark measures which silent failures each bioinformatics tool lets through.

**Status.** The `mutants/` package is planned and enters with the complete silent-failure benchmark of Slice 7 (`ROADMAP.md`). Today the coverage of failure modes comes from the versioned synthetic corpus (§2) and from the tests of each defence, which corrupt the file in the test itself: truncation at several offsets, an HTML page with a data extension, misaligned pair, false BSIZE, BOM. The class × tool matrix does not exist yet, nor does the transport matrix.

The `mutants/` package generates, from valid synthetic files, versions corrupted on purpose. Each mutant class declares which failure mode of `RELIABILITY.md` it represents.

| Class             | Mutation                                                          | Mode    |
| ----------------- | ----------------------------------------------------------------- | ------- |
| `truncate`        | cuts at a random offset, including in the middle of a gzip member | M4      |
| `truncate-record` | removes the whole last record, file remains well formed           | M6, M15 |
| `html-body`       | HTML error page with the format's extension                       | M3      |
| `flip-byte`       | one byte changed at a random offset                               | disk    |
| `shift`           | bytes inserted or removed in the middle                           | disk    |
| `short-mate`      | R2 with shortened reads, valid structure                          | M15     |
| `pair-desync`     | one read removed from R2 only                                     | M5      |
| `pair-swap`       | R2 of another sample                                              | M5      |
| `chrom-rename`    | `chr1` → `1` in the annotation                                    | M11     |
| `release-swap`    | GTF of another release with the same name                         | M1      |
| `superseded`      | source publishes a different checksum at the same URL             | M2      |
| `suppressed`      | source marks the run as suppressed                                | M16     |
| `chemistry-swap`  | R1 of 10x v2 with metadata declaring v3                           | M17     |
| `matrix-colswap`  | two matrix columns with swapped names; one column removed         | M18     |
| `duplicate-run`   | same content under two accessions                                 | M19     |

Two uses:

- **Project suite.** Every mutation of every class that Colligo declares detectable is detected, with the right exit code. A new mutant class enters together with the defence that catches it.
- **Benchmark.** The same matrix runs against other tools: `gzip -t`, `samtools quickcheck`, FASTQ validators, `md5sum -c` against the source's MD5, iSeq, `wget -c`, `curl -C -`, `prefetch`. Output: a class × tool table with detected or not, and time to detection.

Transport matrix, maintained: `{Range ignored, truncated body, reset in the middle, 429, 412, ENOSPC}` × `{Colligo, wget -c, curl -C -, prefetch}`, with successful resume rate, bytes retransmitted and throughput.

Runs against a local server, with no external network. Output in a versioned table, so that a regression shows up in a diff.

---

## 7.1 Performance

Speed is a measured requirement, not an impression. A versioned benchmark, running on the reference machine declared in the result itself (CPU, disk, filesystem).

| Measurement                                | Comparison floor                                             | Goal                         | Measured today?                                 |
| ------------------------------------------ | ------------------------------------------------------------ | ---------------------------- | ----------------------------------------------- |
| Full `check` of `.fastq.gz`                | pure-Go gunzip (`klauspost/compress`), same file and machine | ≤ 110% of the floor; `zcat \ | wc -l` measured and reported                    |
| Full `check` of BAM                        | `samtools view -c`                                           | ≤ 110% of the floor          | no; BAM does not have a validator yet           |
| Hash of an uncompressed file               | `b3sum`                                                      | ≤ 120% of the floor          | yes, `TestPerformanceHash`; goal not met (§7.1) |
| Second run, 10 thousand unchanged files    | —                                                            | ≤ 5 s on SSD                 | no; depends on the `stat` gate (Slice 4)        |
| Allocations per record in the FASTQ parser | —                                                            | 0                            | yes, `testing.AllocsPerRun` in `format`         |
| `get` with full verification               | plain `curl`, same file and local server                     | ≤ 105% of the floor          | no; `get` is Slice 3                            |

The reference machine and the baseline live in `testdata/perf/<machine>/`: `machine.md` (hardware, governor, power), `baseline.json` (three recorded runs per test) and the CPU and memory profiles `cpu.pprof` and `mem.pprof`. The profiles are built with `-trimpath` and scrubbed of the binary path, and nothing with a user name or a directory is committed.

**Regression gate.** With `COLLIGO_PERF_MACHINE=<machine>`, `TestPerformanceFloor` and `TestPerformanceHash` compare the gated measurements (the time of `Compute`, and for gzip also its ratio to each floor) with the best of the recorded runs, and fail when every one of three rounds is more than 5% worse. Interference on a machine only makes a run slower, so the best time is the one that estimates the machine, and a single noisy round does not fail the test. The ratio against `b3sum` is reported and not gated: `b3sum` finishes in under 0.2 s, and a few milliseconds of noise there move the ratio by tens of percent. It is a local check on the reference machine and never runs in CI: the GitHub runner is a different, shared machine in every run, so a time recorded here means nothing there and the variation between runners is larger than 5%. With `COLLIGO_PERF_RECORD=1` the run is appended to the baseline instead.

On the reference machine the best-of-3 of the same measurement varied by up to 8% between runs, so the 5% gate is only meaningful on a quiet machine.

The `Hash of an uncompressed file` goal is reported, not enforced: `Compute` also computes MD5 and SHA-256 and validates the format in the same read, and `b3sum` does only BLAKE3.

The switch from `compress/gzip` to `klauspost/compress/gzip` only enters with the result of this benchmark showing a gain.

---

## 7.2 Resources and limits

`Compute` is measured against hostile or large input, so that a memory regression breaks the suite instead of showing up on a server.

| Test                                       | Input                                                                      | What it asserts                                                                                                                                                      |
| ------------------------------------------ | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| gzip with a huge ratio                     | 2 GiB of zeros, ~2 MiB compressed, generated in streaming                  | terminates, exact `uncompressed_bytes`, heap peak ≤ 32 MiB above the baseline                                                                                        |
| 20 million short reads (`COLLIGO_SLOW=1`)  | ~1.1 GiB generated in streaming, lengths 20 to 24                          | exact histogram, `read_ends`, MD5 and counts; peak ≤ 32 MiB; last quarter does not grow                                                                              |
| file that shrinks or grows during the read | truncated or appended when half was read                                   | `Compute`: size and hashes are of the bytes actually read and it does not flag the change; what flags it is `CheckFile` (`changed_during_read`, `RELIABILITY.md` M2) |
| allocations per MiB read                   | plain FASTQ and gzip FASTQ, slope between a small and a large input        | ≤ 8 allocations and ≤ 16 KiB allocated per additional MiB                                                                                                            |
| single read                                | `CheckFile` and `CheckAll`, measured by `rchar` of `/proc/self/io` (Linux) | each byte read exactly once; `Discover` reads at most 64 KiB per file                                                                                                |

The limits start from the structural floor, not from what today's machine does: the read ring has 8 buffers of 1 MiB and the decompressor plus the extractor stay below 1 MiB, so the measured peak is between 8 and 13 MiB and the 32 MiB ceiling leaves headroom for the garbage collector. Retaining more than 1.5% of the decompressed output (32 MiB of 2 GiB) already exceeds the ceiling. The measured allocation slope is zero within noise (at most one allocation and a few hundred bytes per MiB); the limits catch one allocation per record (thousands per MiB) or a new buffer per chunk (1 MiB per MiB).

`COLLIGO_SLOW=1` turns on the tests that take minutes (20 million reads: about 9 s without `-race` and 50 s with). Without the variable they skip and the message says how to turn them on. CI does not set it.

---

## 8. Mechanical rules

None of them depends on someone remembering.

| Rule                               | Tool                                              |
| ---------------------------------- | ------------------------------------------------- |
| Core does not import `source/*`    | test with `x/tools/go/packages`, or `depguard`    |
| Light door does not import `store` | same import-graph test                            |
| Raw `string` where `secret.*` fits | own `go/analysis` analyzer                        |
| No `init()` with side effects      | custom lint                                       |
| Stable public surface              | `apidiff` against the last tag                    |
| Race                               | `go test -race -count=2 ./...`                    |
| Goroutine leak                     | `goleak` in every concurrency test                |
| Secret in the diff                 | `gitleaks` in pre-commit and in CI                |
| Tracked real fixture               | `git ls-files` matched against forbidden patterns |
| Real assertion coverage            | `gremlins` (mutation), quarterly, core only       |
| No non-configured host             | instrumented dialer, across the whole suite       |

`-count=2` catches state leaking between runs. Mutation score is the only metric that answers "would my suite catch a hidden bug?" — coverage only says how much code ran.

---

### External tools

Some tests compare Colligo with programs that are not ours: the verifier `tools/verify_cert.py` (Python with the `blake3` package) and `b3sum`. A test that depends on them never passes silently without having run:

| Situation                                                         | Result                                                          |
| ----------------------------------------------------------------- | --------------------------------------------------------------- |
| `COLLIGO_PYTHON` set and the interpreter does not import `blake3` | fails                                                           |
| `COLLIGO_REQUIRE_PYTHON=1` and no Python with `blake3`            | fails                                                           |
| nothing set and no Python with `blake3`                           | skips, and the message says how to turn the skip into a failure |
| `COLLIGO_REQUIRE_B3SUM=1` and no `b3sum`                          | fails                                                           |
| no `b3sum` and no variable                                        | the comparison does not run and the test logs that              |

CI sets `COLLIGO_REQUIRE_PYTHON=1` and `COLLIGO_REQUIRE_B3SUM=1`. The performance benchmark is opt-in via `COLLIGO_PERF=1`, because it measures speed and not correctness. The resource tests that take minutes are opt-in via `COLLIGO_SLOW=1`, because they cost time and no extra correctness in day-to-day work.

### `COLLIGO_*` variables

| Variable                 | Effect                                                                                                                                                           |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `COLLIGO_PYTHON`         | path of the Python interpreter with `blake3` used by the `tools/verify_cert.py` tests; if set and the interpreter does not import `blake3`, it fails             |
| `COLLIGO_REQUIRE_PYTHON` | `1` turns the absence of Python with `blake3` into a failure, instead of a skip                                                                                  |
| `COLLIGO_REQUIRE_B3SUM`  | `1` turns the absence of `b3sum` into a failure, instead of a log record                                                                                         |
| `COLLIGO_PERF`           | any non-empty value turns on `TestPerformanceFloor`; without it the test skips                                                                                   |
| `COLLIGO_PERF_DIR`       | directory where the benchmark stores and reuses the generated `.fastq.gz`; without it, it uses a temporary directory of the test and generates again on each run |
| `COLLIGO_PERF_MACHINE`   | name of the directory in `testdata/perf/` with the baseline of this machine; turns on the 5% regression gate of §7.1                                             |
| `COLLIGO_PERF_RECORD`    | any non-empty value appends the run to `baseline.json` instead of comparing (needs `COLLIGO_PERF_MACHINE`)                                                       |
| `COLLIGO_SLOW`           | any non-empty value turns on the resource tests that take minutes (§7.2)                                                                                         |

### Performance benchmark

```
COLLIGO_PERF=1 COLLIGO_PERF_DIR=/path/with/space go test ./facts -run TestPerformance -v
```

The test generates a `.fastq.gz` with 1 GiB of decompressed FASTQ (100-base reads, fixed seed), measures three times (best time) the pure-Go gunzip, `zcat | wc -l` and `Compute` with hashes, and prints the three times and the ratios. It fails if `Compute` exceeds 110% of the pure-Go floor; the ratio against `zcat | wc -l` is only reported. `TestPerformanceHash` generates 1 GiB of uncompressed FASTQ and measures `Compute` with hashes against `b3sum` and `b3sum --num-threads 1`; the ratio is reported, the goal of 120% is not enforced. It needs `b3sum`, and `COLLIGO_REQUIRE_B3SUM=1` turns its absence into a failure. `TestPerformanceFloor` requires `zcat` on the `PATH`. `go test ./facts -bench ComputeFastqGz` measures the same `Compute` over a generated file with 256 MiB decompressed as a plain Go benchmark, with no goal.

---

## 9. What CI runs

| Check                                                                                                                                         | When          | Runs today / planned         |
| --------------------------------------------------------------------------------------------------------------------------------------------- | ------------- | ---------------------------- |
| branch name, PR title and message of each commit (`.github/check-names.sh`, which also self-tests)                                            | PR            | runs today                   |
| `gitleaks` over the history                                                                                                                   | PR and `main` | runs today                   |
| tracked real fixture (`git ls-files testdata/real`)                                                                                           | PR and `main` | runs today                   |
| `gofmt`, `go build`, `go vet`                                                                                                                 | PR and `main` | runs today                   |
| `secret` type analyzer (`noleak`)                                                                                                             | PR and `main` | runs today                   |
| `go test -race -count=2 ./...`, which includes the import-graph and two-doors rule tests, the Python verifier and the comparison with `b3sum` | PR and `main` | runs today                   |
| `Fuzz*` targets running only the corpus seed (inside `go test`)                                                                               | PR and `main` | runs today                   |
| lint beyond `vet`                                                                                                                             | PR            | planned                      |
| `sourcetest` of all public sources                                                                                                            | PR            | planned, Slice 2 onwards     |
| Colligo's mutant suite                                                                                                                        | PR            | planned, Slice 7             |
| fuzz 60 s per target (`fuzz` job, parallel to `test`, informative, not required)                                                              | PR            | runs today                   |
| `apidiff` against the last tag                                                                                                                | PR            | planned, after the first tag |
| `COLLIGO_SLOW=1 go test -race ./...` (`slow` job of `weekly.yml`)                                                                             | weekly        | runs today                   |
| fuzz 30 min per target, corpus cached between weeks (`fuzz` job of `weekly.yml`)                                                              | weekly        | runs today                   |
| fixture drift, mutant benchmark against external tools                                                                                        | weekly        | planned                      |
| performance benchmark and 5% regression gate (`TestPerformance*`)                                                                             | by hand       | never in CI                  |
| mutation testing                                                                                                                              | quarterly     | planned                      |

CI runs on `pull_request` and on `push` to `main`, never on the push of a branch without a PR. The weekly workflow runs on `schedule` and `workflow_dispatch`. GitHub disables scheduled workflows after 60 days without activity in the repository; a quiet repository needs a manual `workflow_dispatch` (or any commit) to bring the weekly job back.

**Never in CI:** any test with the `restricted` build tag.
