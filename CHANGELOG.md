# Changelog

All notable changes to Colligo. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the major version is 0, any minor release may change the interface.

## [Unreleased]

### Added

- `transport`: an HTTP client for metadata with a per-domain rate limit, retry with exponential backoff and jitter, `Retry-After`, a per-attempt timeout, a bound on the response size and a typed error for an unexpected content type. Byte ranges are refused until the resume slice.
- `source/ena`: the metadata part of the ENA adapter. It reads the Portal API `filereport` for `read_run` through an injected `transport.Transport`, returns one `model.Sample` per run with one `model.Artifact` per file, declares `read_count` and `base_count` per run, and keeps the raw response. A response that is not a table, a missing column, misaligned MD5, size and URL lists and an unknown run are typed errors or validation issues, never a silent zero value. An empty count is not declared (`nil`), not zero, and an empty description or file list keeps the run, so a suppressed run still carries its status.
- `model.Sample`, `Artifact`, `Experiment`, `RunCounts`, `RawResponse`, `ValidationIssue`, `SourceError` and `NotFoundError`.
- `colligo identify`: names a FASTA, plain or gzip/BGZF compressed, by its GA4GH Sequence Collection digest (Refget Sequence Collections v1.0.0, level 0), taken from the same single read that validates the file. `--json` has its own schema (`schema_version` 1). A file that matches no known reference is `not_identified`, not an error (exit 0); a structurally broken FASTA is `invalid` (exit 3). The table of known references is embedded and empty until a digest can be checked against an official source.
- `identity` package: `sha512t24u`, RFC 8785 canonical form, the streaming `Collector`, and a known-vector self-test that runs before any digest is trusted.
- The `seqcol` fact (`seqcol-1`), added to the read only when `facts.Options.Identify` is set, so `check` does not pay for it.
- `colligo check --samplesheet <file.csv>` checks an nf-core samplesheet (the nf-core/rnaseq format, schema 3.27.0, or the generic `sample,fastq_1,fastq_2`) against the content of the files it lists: every file is read once, the mates are the ones the samplesheet declares, and a swapped, missing, repeated or shared file is reported with the line it is on. Lanes of one sample are legitimate. A remote location (`s3://`, `https://`) is not fetched and is `blocking`. An unknown format, a missing column or a malformed CSV is a usage error (exit 1). Its `--json` output is the one of `check`.
- Diagnoses `sheet_remote_path`, `sheet_shared_file`, `sheet_duplicate_row` and `sheet_mates_swapped`.
- `colligo check --source ena:<accession> <path>...` compares the files with what ENA declares for the accession (a run, experiment, sample or study), following RELIABILITY M15 and M16. The bases summed over every file of a run are the blocking comparison (`count_mismatch`, exit 3), which is what catches a valid FASTQ with a shortened read 2. `read_count` passes when it matches one of the three readings seen in real runs (each file, the sum of the files, the pairs plus the unpaired files) and is only `advisory` (`read_count_unexplained`) when none does. A run labelled single whose files form a pair gets the advisory `layout_mismatch`. A status other than `public` is `blocking` (`source_status`). A count the source left empty is reported as not declared and skipped, never compared as zero. A local file stands for the declared file of the same name, or of the same name plus `.gz`; only an exact name is held to the published MD5 and size. A declared file with no local copy (`source_file_missing`, blocking), a local file the run does not list (`source_file_unlisted`, advisory), a name claimed by several copies (`source_file_ambiguous`, blocking) and the defects of the source response (`source_row_rejected`, `source_field_empty`) all appear in the report. A file whose run fails gets `source_run_failed` and is not `verified`. A network or source failure is fatal (exit 2) and nothing is reported as verified.
- Diagnoses `source_status`, `source_file_missing`, `source_file_unlisted`, `source_file_ambiguous`, `read_count_unexplained`, `layout_mismatch`, `source_row_rejected`, `source_field_empty` and `source_run_failed`.

### Changed

- The `--json` output of `check` and `validate` is now `schema_version` 2: it gains the `source` object, present only with `--source`. Nothing else changed in the document.
- The `count_mismatch` observation says "The content has" instead of "The file has", since it now also describes a run.

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
