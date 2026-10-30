# Changelog

All notable changes to Colligo. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the major version is 0, any minor release may change the interface.

## [Unreleased]

To be released as `0.1.0` when slice 1 closes.

### Added

- Typed secrets (`secret.Secret`, `secret.Name`, `secret.Endpoint`) that never print or serialise a restricted value, and the `noleak` analyzer that fails the build when a path, URL or credential travels as a raw string.
- Known-vector self-test of BLAKE3, MD5 and SHA-256 before any check.
- Single-read pipeline: one read of each file yields the BLAKE3 tree in 8 MiB groups (Bao outboard compatible), MD5, SHA-256 and the format facts, with every consumer in its own goroutine.
- Structural validators that also extract facts: gzip, BGZF (block-size chain and EOF block), FASTQ, FASTA and GTF.
- `colligo check` and `colligo validate` for loose files and directories, with `--expected`, `--json`, `--quick`, `--cross-check` (`sha256sum`/`b3sum` format), `--certificate` and exit codes 0 to 4.
- Assertions evaluated in the defence precedence order, pair invariants for R1/R2, and a diagnoses catalogue that turns each failure into an observation, a probable cause and a suggested action.
- Certificates that a third party can check with `tools/verify_cert.py`, which imports nothing from Colligo.
- A versioned synthetic corpus whose expected results come from the generator, not from a parser.
