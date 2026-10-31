# Changelog

All notable changes to Colligo. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the major version is 0, any minor release may change the interface.

## [Unreleased]

### Added

- `transport`: an HTTP client for metadata with a per-domain rate limit, retry with exponential backoff and jitter, `Retry-After`, a per-attempt timeout, a bound on the response size and a typed error for an unexpected content type. Byte ranges are refused until the resume slice.
- `source/ena`: the metadata part of the ENA adapter. It reads the Portal API `filereport` for `read_run` through an injected `transport.Transport`, returns one `model.Sample` per run with one `model.Artifact` per file, declares `read_count` and `base_count` per run, and keeps the raw response. A response that is not a table, a missing column, misaligned MD5, size and URL lists and an unknown run are typed errors or validation issues, never a silent zero value. An empty count is not declared (`nil`), not zero, and an empty description or file list keeps the run, so a suppressed run still carries its status.
- `model.Sample`, `Artifact`, `Experiment`, `RunCounts`, `RawResponse`, `ValidationIssue`, `SourceError` and `NotFoundError`.

## [0.1.0] - 2026-10-30

First release: slices 0 and 1, the light door. One command, no database, no configuration.

### Added

- Typed secrets (`secret.Secret`, `secret.Name`, `secret.Endpoint`) that never print or serialise a restricted value, and the `noleak` analyzer that fails the build when a path, URL or credential travels as a raw string.
- Known-vector self-test of BLAKE3, MD5 and SHA-256 before any check.
- Single-read pipeline: one read of each file yields the BLAKE3 tree in 8 MiB groups (Bao outboard compatible), MD5, SHA-256 and the format facts, with every consumer in its own goroutine.
- Structural validators that also extract facts: gzip, BGZF (block-size chain and EOF block), FASTQ, FASTA and GTF.
- `colligo check` and `colligo validate` for loose files and directories, with `--expected`, `--json`, `--quick`, `--cross-check` (`sha256sum`/`b3sum` format), `--certificate` and exit codes 0 to 5 (5: `--quick` found nothing in what it read, content not read in full).
- Assertions evaluated in the defence precedence order, pair invariants for R1/R2, and a diagnoses catalogue that turns each failure into an observation, a probable cause and a suggested action.
- Certificates that a third party can check with `tools/verify_cert.py`, which imports nothing from Colligo.
- A versioned synthetic corpus whose expected results come from the generator, not from a parser.
- A performance baseline on a documented reference machine: a full `check` of a `.fastq.gz` at 106% of pure-Go gunzip (target 110%), and the three hashes at 110% of the slowest one alone (target 120%), measured as the median of 5 runs under recorded conditions.
- Continuous integration on every pull request (gitleaks, gofmt, vet, `noleak`, `go test -race` with the Python verifier and `b3sum` required) plus a weekly job with the slow tests and 30 minutes of fuzzing per target.

[Unreleased]: https://github.com/colligo-go/colligo/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/colligo-go/colligo/releases/tag/v0.1.0
