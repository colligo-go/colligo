# Changelog

All notable changes to Colligo. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the major version is 0, any minor release may change the interface.

## [Unreleased]

### Added

- `colligo identify`: names a FASTA, plain or gzip/BGZF compressed, by its GA4GH Sequence Collection digest (Refget Sequence Collections v1.0.0, level 0), taken from the same single read that validates the file. `--json` has its own schema (`schema_version` 1). A file that matches no known reference is `not_identified`, not an error. The table of known references is embedded and empty until a digest can be checked against an official source.
- `identity` package: `sha512t24u`, RFC 8785 canonical form, the streaming `Collector`, and a known-vector self-test that runs before any digest is trusted.
- The `seqcol` fact (`seqcol-1`), added to the read only when `facts.Options.Identify` is set, so `check` does not pay for it.

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
