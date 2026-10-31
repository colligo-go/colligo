# Colligo — Roadmap

> **Current slice: 2 — Declared against the source.** Slices 0 and 1 are closed and released as `v0.1.0`. Nothing outside Slice 2 should be built yet.

The rule of this document: build a thin slice end to end before generalising in breadth.

Two rules learned along the way:

- **What is structural comes before what is functional — but only what cannot be fitted in later.** Types that do not leak and verification of the tool itself come first. Diagnosis, reporting and the full regime for restricted data are added later without pain, and are left for when there is a restricted source.
- **The light door comes before the heavy one.** The first useful delivery has no database. It is what generates users and what exercises the core — extractors, validators, adapters — before persistence exists. See `DESIGN.md` §1, "Two doors".

---

## Slice 0 — Minimal foundation (closed)

Small in lines, impossible to add later.

Scope:

- `secret/`: `Secret`, `Name`, `Endpoint`, with redaction by construction.
- `logf`: typed fields; no generic `string` field for path, URL or credential.
- `checksum/`: known vectors of BLAKE3, MD5 and SHA-256 run at start-up.
- A `go/analysis` analyzer in CI that forbids a raw `string` where the domain requires `secret.*`.

Definition of done:

- `%v`, `%s`, `%+v`, `%#v` and `json.Marshal` of every `secret/` type do not contain the sentinel.
- Known vectors fail loudly when deliberately sabotaged.
- The analyzer rejects a code fixture that passes a `string` to `logf.Name`.

Moved to later: `doctor`, `redact-report`, canary, sync-folder detection, database permissions, instrumented dialer, global sentinel. They enter in Slice 6, together with the first restricted source, or in Slice 7.

---

## Slice 1 — Light door: `colligo check` (closed, `v0.1.0`)

The smallest useful unit. No database, no TOML, no state.

Scope:

- `facts/`: a single streaming read that produces BLAKE3 root (Bao outboard, 8 MiB group), MD5, SHA-256 and the format facts.
- `format/`: validators for gzip, bgzf, fastq, fasta and gtf, which also extract facts — count of records and bases, first and last ID, sequence names.
- Pair invariant over the facts: same count, compatible IDs.
- `cmd/colligo check` and `validate`: loose files, `--expected`, `--json`, exit codes of `RELIABILITY.md` §1.
- `--cross-check` with output compatible with `b3sum` and `sha256sum`.
- Single read distributed in parallel, without `io.MultiWriter`; allocation-free FASTQ parser; `check --quick`.
- Performance benchmark against the pure-Go gunzip floor, with `zcat | wc -l` reported (`TESTING.md` §7.1).
- `check .` with format and pair discovery; `diagnoses/` catalogue with one entry per assertion.
- `check --certificate` and the independent verifier `tools/verify_cert.py`.

Definition of done:

- Truncate a `.fastq.gz` at 10 offsets: all detected.
- HTML page renamed to `.gtf.gz`: rejected by the magic bytes.
- R2 with one read fewer than R1: `blocking`, code 3.
- Each file is read exactly once, verified by a bytes-read counter in the test.
- Hashes match `b3sum` and `sha256sum` when present.
- A full `check` of a `.fastq.gz` at most 10% above pure-Go gunzip on the reference machine.
- FASTQ parser with zero allocations per record.
- `colligo check .` on a folder with R1/R2 pairs and a truncated file runs without configuration and prints the diagnosis in a sentence.
- The Python verifier agrees with the certificate across the whole synthetic corpus.

---

## Slice 2 — Declared against the source (current)

The light door starts asking the source.

Scope:

- `source/ena/`: only the metadata part — `fastq_md5`, `fastq_bytes`, `read_count`, `base_count`, record status. Pure, with a synthetic fixture.
- Minimal HTTP `transport/` for metadata, with per-domain rate limit.
- `colligo check --source ena`: compares facts against the declared. M15 of `RELIABILITY.md`: the layout comes from the content, not from the source label, and a label that contradicts the content is reported as `layout_mismatch`.
- `colligo identify`: Sequence Collection digest for FASTA; GENCODE GTF release by the header; hash against official checksums where they exist.
- `colligo check --samplesheet`: nf-core pipeline samplesheet checked against the content.
- MultiQC module that reads the `--json` of `check` and shows the result in the report the user already opens.

Definition of done:

- The QC Fail case reproduced in a fixture — valid FASTQ with a shortened read 2 — exits with code 3 because `base_count` does not match.
- The recorded ENA responses of DRR021374, SRR10641224 and ERR5060520 are fixtures: the first two pass with `read_count` per file, and the third passes with a `layout_mismatch` advisory instead of a `blocking`.
- `identify` recognises a GENCODE GTF by the header and a FASTA by the seqcol digest.
- A samplesheet with R2 swapped between two samples is rejected.
- The result of `check` appears in a MultiQC report.
- Distribution: bioconda recipe drafted and nf-core module sketched.

---

## Slice 3 — Heavy door: fetch, store and resume

The acquisition core, now with persistence.

Scope:

- Before the code: TLA+ model of resume, rename and the state machine, verified by model checking (`RELIABILITY.md` M20).
- `colligo get`: accession → download → `samplesheet.csv` + `colligo.lock`, without a database, with resume state next to the files.
- Verification overlapped with the download; adaptive concurrency per domain (SPEC §12).
- Database opening per `CONTRACT.md` §2, identical to Scribo's: journal mode by filesystem, `_txlock=immediate`, `tool_runs` with the contract's DDL.
- SQLite `model/` and `store/` per SPEC, with PRAGMAs in the DSN read back and the `facts` table.
- Writer goroutine with error and flush; `tool_runs` with `run_id` in every event; detection of an interrupted run.
- `source/gencode/` with `Fetch` and `Resolve`, pure.
- `transport/` with `Range`, `If-Range`, retry and rate limit.
- `downloader/`: streaming to `.part` feeding the same fact extraction as Slice 1; several connections per file aligned to groups, off by default.
- Resume with reconciliation against the disk using the Bao outboard.
- `cmd/colligo` with `fetch`, `verify` and `status`.

Definition of done:

- Download GENCODE FASTA and GTF end to end, validate, rename.
- `colligo get` of a small ENA study delivers a validated samplesheet and lock.
- `get` with full verification at most 5% above plain `curl`.
- A fake server with limited capacity and 429: adaptive concurrency converges without exceeding the ceiling and without a permanent error.
- Crash at any point: all invariants of the model hold after each resumption.
- Kill the process in the middle and resume, with a correct final checksum.
- Resume property test passing.
- `ENOSPC` injected: safe offset written before the error.
- `go test -race -count=2` clean.

---

## Slice 4 — `adopt`, scrub, manifest and attestation

What unlocks the terabytes that arrive from outside.

Scope: `adopt/` individual and in batch by CSV; `stat` gate that decides what to read, without declaring anything verified; amortised scrub by `last_verified_at`; `manifest/` with root and outboard; `attest`; `colligo lock` and `check --lock`; duplicate across accessions by root (M19).

Criterion: adopt a file coming from `prefetch` and reverify it later pointing at the corrupted slice; export a manifest, corrupt one byte, and the attestation points at the exact index; change the release of a reference and the diff of `colligo.lock` shows only that line; a second run over 10 thousand unchanged files within the target of `TESTING.md` §7.1.

---

## Slice 5 — Watching the source, GEO, two-phase validation

Scope:

- `colligo recheck --upstream`: re-runs the metadata part of the adapters and compares with what was stored. M16 of `RELIABILITY.md`.
- `source/geo/` via E-utilities; small-file path; profiles `rnaseq`, `tabular` and `singlecell`.
- Integration with Scribo (SPEC §14.1): `input_signals` signals in custody, completeness funnel with `[results]`, scrub prioritised by dependents.
- Single-cell chemistry declared × observed (M17) and coherence of the supplementary matrix against the GSMs (M18).
- Local Phase 1 and remote Phase 2 validation; `discrepancies` with severity; `expected_set_hash`; `status --require-complete`.

Criterion: a fixture in which the run goes from public to suppressed generates a `blocking` `discrepancy`; 10x v2 data declared as v3 is `blocking`; a matrix with two columns with swapped names is `blocking`; declaring the wrong type for the pipeline in strict mode; removing a file from the fixture and the study ceasing to be complete.

---

## Slice 6 — Reference, `compat` and the first restricted source

Scope:

- `reference_assets` with assembly, release, `chromosome_naming` and seqcol digest.
- `compat/`: comparison by `sorted_name_length_pairs` between FASTA, `.fai`, BAM, VCF; name inclusion for GTF and BED.
- First restricted-source adapter, with the full regime of `CONFIDENTIALITY.md`: permissions, sync folder, global sentinel, instrumented dialer, canary.

Criterion: a GENCODE FASTA against a VCF with Ensembl contigs returns incompatible with the name diff; declaring the wrong release in the TOML and receiving `fatal`; every invariant of `CONFIDENTIALITY.md` §6 with a passing test.

---

## Slice 7 — Breadth, minimal derivatives, benchmark and polish

Scope: `source/cbioportal/`, `source/pdb/`, `source/dfam/`, `source/ensembl/`; `structure` profile; isolated FTP; `drs://`; minimal `derived_files`; `exporter/` with custody; parallel BGZF decompression; `doctor` and `redact-report`; complete silent-failure benchmark (`TESTING.md` §7); GoDoc; executable examples; README with a real study.

---

## Data types by version

```
v1 — rnaseq, singlecell, tabular, reference, structure, chipseq, generic
v2 — define according to real use of v1, not on paper now
```

`atacseq`, `methylation` and `wgs` live in `BACKLOG.md`. v1 teaches which types appear in real studies — those are better candidates than the ones that seem important today.

---

## Version milestones

```
v0.x.x — interface under development, may change
v1.0.0 — public interface frozen, strict semver
```

It stays at v0 until the interface is stable and tested in real use. The light door can get its own v0 releases from Slice 1.

**The version comes only from the git tag.** There is no version constant in the code. Each closed slice bumps the minor: Slice 1 → `v0.1.0`, Slice 2 → `v0.2.0`. A fix published over a release bumps the patch: `v0.1.1`. Between tags, the binary identifies itself by Go's pseudo-version, with date and commit (`v0.0.0-20261029173537-1d27b8e84ac6` before the first tag), and with `+dirty` when it was built with an uncommitted change. `colligo version` and every certificate record version and commit, so that any result points to the exact code that produced it.

Tag only from `main` with green CI, annotated, with the release notes taken from `CHANGELOG.md`.

The repository is public since Slice 0. Not out of ideology: developing closed makes the project ineligible for the venues that ask for a public development history, and each closed month is a month that does not count.
