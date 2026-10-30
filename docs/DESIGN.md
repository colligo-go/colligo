# Colligo — Design and Decisions

The purpose of this document is to record **why** Colligo is the way it is. How to implement is in `SPEC.md`. Defences against silent error are in `RELIABILITY.md`. Protection of restricted data is in `CONFIDENTIALITY.md`. What comes next is in `ROADMAP.md` and `BACKLOG.md`. What already exists and where Colligo fits is in `RELATED.md`.

---

## 1. Vision

Colligo is a Go library and a CLI that answer one question about genomic data:

> Do I have the data I think I have?

The name comes from the Latin _colligere_ — "I collect, I gather, I bring together".

The distinction matters and defines the project. Downloading public data is a solved problem: iSeq, fetchngs, ffq, pysradb, fastq-dl and others do it well, with MD5 and resume. Guaranteeing that the **bytes** stay the same over the years is also solved, outside bioinformatics: git-annex (`fsck --incremental-schedule`), DataLad and DVC do it. See `RELATED.md`.

What none of those tools knows is what the file **means**. That the `.fastq.gz` ended where it should. That it has the number of reads and bases that the source published. That R1 and R2 are a pair. That the GTF is GENCODE 44 with `chr1`, and not Ensembl 111 with `1`. That 47 of 50 is not complete. That the source suppressed the run as incorrect after you downloaded it.

Colligo does three things, always on data with bioinformatics semantics:

- **Identify:** which file this actually is — release, assembly, sequence collection — deduced from the content, not from the name.
- **Verify:** it is whole and it is what the source and you declared.
- **Watch:** it remains valid, on disk and at the source.

In one sentence: Colligo does not prove that you downloaded the right bytes — everybody does that. It proves that you have the data you think you have, and warns you when that stops being true.

**It does:** extracts facts from the content in a single read, compares the declared against the observed, fetches metadata from public and private sources, organises the metadata into a typed experimental structure, downloads with integrity and resume, **adopts** a file that arrived from outside, persists everything in SQLite with an auditable history, exports a portable manifest, attests a collection against a third party's manifest and re-queries the source to detect change.

**It does not:** analysis, pipeline management, visualisation, encryption at rest, anonymisation of content.

### Two doors, same library

**Light door — `colligo check`.** One command, no database, no TOML, no saved state. It reads each file once, extracts the facts and, with `--source`, compares against what the source declared. A non-zero exit code if something does not match; `--json` for machines. Distributed via bioconda and as an nf-core module. It is the door that generates users: it does not require reading any documentation.

```
colligo check SRR2133898_1.fastq.gz SRR2133898_2.fastq.gz --source ena
```

**Heavy door — `fetch`, `adopt`, `status`, `manifest`, `attest`, `recheck`.** SQLite database, recorded runs, watching over time, manifest, confidentiality. For those who need custody: core facilities, reanalysis at scale, consortia, controlled data.

**Rule:** the light door depends on nothing from the heavy one. Fact extractors, format validators and the metadata part of the adapters live in the core and serve both. The heavy one only adds persistence and time.

### Who uses it

Those who reanalyse public data at scale (meta-analysis, atlas, model training), those who develop and compare methods, clinical groups with controlled data, core facilities, consortia that exchange collections, and those who need to reproduce their own thesis years later. The student who downloads six samples once uses the light door and does not need to know the heavy one exists.

---

## 2. Principles

**Declared against observed, at every layer.** Every Colligo check compares what someone claimed with what the content shows. The source declares MD5, `read_count`, `base_count` and the set of files; you declare type, release and assembly; the file name declares the pair. The file is the witness. No tool crosses the three parties — source, declaration and content — and follows this over time. That is the differentiator.

**The user is the source of truth about intent.** They declare the data type before starting. The API confirms, it does not discover. The content confirms or contradicts.

**Fail closed.** "Never silent" implemented as a warning produces warnings that nobody reads. A discrepancy that compromises the result **stops** the pipeline. Strict mode is the default; `--permissive` changes whether it stops, never whether the information exists.

**Partial data has value.** 50GB downloaded up to 80% is hours of transfer. Partial progress is never deleted. This includes the `ENOSPC` path: the safe offset is written before the error is propagated.

**Total auditability.** Every state transition and every API call is recorded with a timestamp and linked to the run that caused it.

**Protection by type, not by discipline.** Restricted data does not leak because the wrong path does not compile, not because someone remembered to redact.

**Extensibility by design, with powerless adapters.** The core is agnostic to the experiment type. Adapters are pure code with no access to disk or database — the worst possible bug in an adapter is returning wrong metadata.

---

## 3. Settled decisions and their whys

### Adapters are pure

The original interface gave `Download` to the adapter, which means the contributor's code writes to disk and to the database. A hidden bug in it can write an absolute path, skip a state transition, or write outside the study directory. No contract test fixes that, because the adapter has direct access to the primitives.

Inverted: the adapter receives an accession and returns data. The core is the only one that downloads, hashes, transitions and writes. **The worst possible bug becomes "returned wrong metadata"**, which the validator catches and `discrepancies` records. Isolation by construction, not by discipline.

Cost: an exotic protocol needs a second interface, `Transport`, with a minimal surface, reviewed with core rigour.

Consequence for governance: `source/*` can be merged by any maintainer, precisely because the conformance suite is the gate.

### `adopt` is as important as `fetch`

Controlled data — dbGaP, institutional — does not arrive over HTTP with `Range`. It arrives through `prefetch`, Globus, or a physical disk. All the range/retry/resume machinery does not apply.

This looked like a limitation and is the opposite: it reveals that the product is not `download`, it is **custody**. `adopt` registers a file that arrived by any means, builds the tree and extracts the facts, and from then on it has perpetual reverification equal to the downloaded ones. Adoption is registration, not transfer — the file is neither copied nor moved.

Honest consequence: an adopted file without a published checksum has guaranteed integrity **from the moment of adoption**. Colligo cannot claim that it arrived correct. The distinction is recorded in the `adopted` state instead of being dissolved into `done`.

### Facts and assertions

The central structural decision. Everything Colligo knows about a file is split into two classes.

**Facts** are extracted from the content in a single streaming read: BLAKE3 root, MD5 and SHA-256, structural validity, count of records and bases, first and last read ID, sampled sequence names, Sequence Collection digest. A fact is a pure function of the content and the extractor version. That is why it is indexed by `(root, kind, extractor version)` and never recomputed while the root does not change. Two files with the same content share facts for free.

**Assertions** are cheap comparisons between facts, declarations and what the source published: the MD5 matches, the base count matches, R1 and R2 match, the release checks out, the set is complete. The custody state is a function of the assertions, not a column that someone updates.

Consequences:

- **The same hash implies the same structure.** If the root checks out, the structural validation cannot have changed its result. Revalidation only runs when the validator version changes. This matters because, for plain gzip and FASTQ, verifying the end requires decompressing everything.
- **The expensive read pays for everything.** If the FASTQ must be decompressed to validate the structure, the same pass counts reads and bases, records the IDs of the ends and feeds the pair and source invariants. Reading the file twice is a design bug.
- **The light door is fact extraction without persistence.** The heavy one is the same extraction with a database.

### The BLAKE3 tree unifies resume, verification and attestation

BLAKE3 is already a Merkle tree. Instead of an own grid of independent hashes — which would be a second structure that could disagree with the global hash —, Colligo stores the **Bao outboard** encoding: the internal nodes of the tree, with an 8 MiB group, in a separate file. The root of that tree **is** the file's BLAKE3. One structure, one root, consistency by construction.

Resume, damage localisation, scrub and attestation become the same operation: verifying a slice against the root. The manifest carries root and outboard; whoever receives it verifies slices without trusting anyone.

Accepted limitation: corruption that shifts bytes makes all the following slices fail, so repair re-downloads from the point of the error to the end. It remains correct, it only loses the saving in this rare case.

Honest limitation: locating damage at block level only saves something when there is repair. While granular repair is in the backlog, the granularity serves resume and diagnosis, not saving. The tree is not the project's differentiator; it is cheap infrastructure that BLAKE3 already gives.

### Internal integrity is BLAKE3; the source's MD5/SHA is the sole judge

BLAKE3 parallelises one file across several cores and exceeds several GB/s. MD5 stays at ~600 MB/s on one core. Continuous integrity uses the BLAKE3 tree; the source's MD5/SHA is a cross-check of the whole file, computed for free in the same read.

### Comparing `stat` is not verifying

Size and mtime equal to the baseline say that nobody touched the file through the normal channels. They say nothing about silent disk corruption, which changes neither. The `stat` gate exists to decide what needs to be read, never to declare a file verified. Only reading the content updates `last_verified_at`.

### Byte integrity is not enough

A correct checksum does not mean a correct file. A GTF from Ensembl 110 and one from 111 are both intact and produce different results. A truncated `.gz` decompresses partially and many downstream tools accept it without complaint.

That is why there are four layers beyond the hash: **identity** (assembly, release, ETag, sequence collection digest), **structure** (magic bytes and end-of-file verification per format), **pair invariant** (count and IDs of R1/R2) and **source invariant** (read and base count against what the source published). The complete catalogue is in `RELIABILITY.md`, which is the document that answers the fear that originated the project.

### Identity deduced from the content

Declaring `release = "GENCODE 44"` in the TOML is necessary, but it is a declaration. Colligo confirms it from the content when it can:

- **FASTA:** Sequence Collection digest (refget, GA4GH). It is independent of compression, line width and header text. It identifies the collection by what it contains.
- **GENCODE GTF:** the version is in the `##description` line of the header.
- **Ensembl GTF:** the header carries `#!genome-build` and dates, with no release number. Here the identification is by hash against the official checksum files of each release.

Confirmed by the content, `release_confidence` becomes `confirmed`. It also solves the orphan file on the server that nobody knows the release of: `colligo identify file.gtf.gz`.

### Compatibility is a relation between sets

`chr1` against `1` is the visible case of a general problem: files that must share a coordinate system. The Sequence Collections standard defines `sorted_name_length_pairs`, a digest of the coordinate system that ignores order, and a comparison function that returns what exists only in A, what exists only in B, what is common, and whether the order coincides.

`.fai`, the BAM `@SQ` and the VCF `##contig` become name-length pairs and are compared by digest equality. GTF and BED have only names, so they become an inclusion test. Compatibility stops being a heuristic and becomes a well-defined relation. Implemented in `compat/`, on top of the facts already extracted.

### The source also changes

A suppressed ENA run disappears from the search, but remains retrievable by accession. Permanent suppression is for data considered incorrect with no prospect of correction. A pipeline can keep using, without warning, data that the source itself has condemned.

`colligo recheck --upstream` re-runs only the metadata part of the adapters — cheap, with no download, pure — and compares with what was stored: record status, set of files, published checksums. A divergence becomes a `discrepancy`. It is watching on the source side, a complement to the scrub on the disk side.

### Structural validation before the rename

The `.part` only becomes the final name after checksum **and** structural validation. This means that the existence of the file under the final name is itself an assertion: "this file passed everything". No downstream pipeline can pick up a half-validated file by accident, because it does not exist under that name.

### Failing closed, with distinct exit codes

`3` means "finished and is wrong". `4` means "did not finish". A script that treats the two the same loses the difference that matters — and the difference between incomplete and interrupted is precisely the one that decides whether you rerun or investigate.

### Mandatory and immutable classification

A source without a declared `classification` causes a load error. There is no default: a `public` default would leak restricted data by omission, and a `restricted` default would train the user to turn the protection off.

A restricted study is restricted as a whole, not per row, because correlation is information: knowing that two patients are in the same study already says something. And the classification is immutable — promoting a public study to restricted would be a hack, because the data has already been written in the clear. The error instructs the user to create a new study.

### Distrusting the tool itself

If the hash computation has a bug, everything "verifies" and everything is wrong. No internal defence reaches that, because the defence uses the faulty code.

Hence three things that look like paranoia and are not: known hash vectors run on every opening, a canary file verified through the same code path as the real files, and output compatible with `b3sum`/`sha256sum` for checking by an external tool. Two independent implementations of the critical check is the only real defence against a bug in the check.

The canary has a second effect: when it fails, the message says the problem is the environment or the tool, so that you do not go hunting for corruption where there is none.

### Language: Go, with a narrow door to Rust

Go is not the fastest language for the project's bottleneck. Decompression and parsing in Rust or C, with libdeflate, zlib-ng and SIMD, go further; the Rust bioinformatics ecosystem (noodles, needletail) is also more complete, especially for CRAM.

Go was chosen for the rest of the system: network, retry, rate limit and metadata fan-out are natural; a static binary with trivial cross-compilation; instant start-up, which matters when running `check` on thousands of files or as a short pipeline step; packaging as a biocontainer and nf-core module with almost no effort. And development speed, which in a one-person project weighs more than a constant factor of throughput.

The real difference is smaller than it looks. In download the network dominates. On a local file the floor is decompression, and `zcat` is C; Go with `klauspost/compress` is about 20% behind it in the Slice 1 measurement. It is a constant factor, not an order of magnitude.

Language is not a differentiator. The user chooses a tool for what it catches and for the friction of using it; Go reduces the friction, but it is not why anyone uses it. And language does not protect an idea: the `read_count` check fits in a Python script inside any workflow. What cannot be copied quickly is the fact architecture and the benchmark as proof.

**The door to Rust.** If the benchmark of `TESTING.md` §7.1 shows Go decompression too far from the floor, the way out is not to rewrite the project. The boundary already exists: the fact extractor is a pure function from bytes to facts, with no database, network or config. An optional Rust accelerator enters there, as a **separate process** — not cgo, not WASM:

- The product is the Go, complete on its own. The accelerator is used if it is on the PATH; if not, the Go extractor runs.
- The two implementations produce identical facts byte for byte, guaranteed by a differential test and fuzzing between them.
- One boundary only (the extractor), one direction only (Go calls Rust), one contract only (the fact schema, with a shared `extractor_version`).

cgo with a Rust library would be faster at the boundary, but requires a Rust and C toolchain in the build, complicates cross-compilation and kills the pure-Go binary. WASM via wazero avoids cgo, but loses native speed, which was the only reason. A separate process costs milliseconds per file to start, irrelevant next to decompressing gigabytes.

The side effect is the best argument: two independent implementations of the extractor are exactly the M9 defence against a bug in the tool itself. The Rust accelerator is speed and a second opinion in the same piece.

Decision postponed with a trigger in `BACKLOG.md`. Until then, the rule that applies now is the boundary one: nothing outside the extractor may depend on an implementation detail of the extractor.

### SQLite driver: `modernc.org/sqlite` (pure Go, no CGO)

Multi-platform with a simple binary is incompatible with CGO in practice. `modernc` is slower, but the bottleneck is always the network and the disk of the files, never the metadata database. We pay in a currency we have plenty of to buy a currency we lack.

Consequence: the documentation does **not** claim "all stdlib". It is "stdlib + a pure-Go SQLite driver".

### PRAGMAs read back

`foreign_keys=ON` because SQLite does not enforce FKs by default — without it, referential integrity is decorative. `journal_mode` resolved by the filesystem — `WAL` on a local disk, `DELETE` on a network filesystem, where WAL is silent corruption (`CONTRACT.md` §2) —, `synchronous=NORMAL` (safe under WAL, much faster than FULL), `busy_timeout=5000`, write transactions with `BEGIN IMMEDIATE`.

An earlier version forced WAL always. With Scribo detecting a network filesystem and Colligo not, a database shared on NFS would be protected by one tool and corrupted by the other. The rule is now single and lives in the contract.

New: the values are **read back** after the SET. A PRAGMA that did not take effect is exactly the kind of silent failure this project hunts.

A real trap, already fallen into once in this code: `*sql.DB` is a pool. `foreign_keys` and `busy_timeout` hold per connection, so a `db.Exec("PRAGMA …")` configures only the connection that the pool handed over at that moment. The PRAGMAs go in the DSN (`_pragma=` of `modernc.org/sqlite`), which applies them to every new connection, and the read-back runs on every connection opened.

### Confidentiality enters through the types, not through the whole regime

The regime of `CONFIDENTIALITY.md` is the heaviest document of the project, and the first restricted source only appears in Slice 6. Building the whole regime before Colligo downloaded a single file would be Slice 0 delivering nothing usable.

What cannot be fitted in later enters first: the types `secret.Secret`, `secret.Name` and `secret.Endpoint`, and the typed `logf`. If the code is born passing `string`, retrofitting is rewriting. Everything else — permissions, synced folder, global sentinel, instrumented dialer, `doctor`, `redact-report` — is added later without pain and enters together with the first restricted source.

### A single writer goroutine, with error and flush

One goroutine serialises all writes, eliminating contention. It is not fire-and-forget: each event carries a reply channel and the worker waits for the ack — otherwise a full disk goes unnoticed. `fatal` transitions are committed without batching. On shutdown: cancel, drain, commit, fsync, write the outcome of the run, exit.

### Every run is recorded

`tool_runs` records version, git SHA, config hash and exit code. Every file event points to the run that caused it.

Without this, the audit says "what happened" but not "with which code" — and "with which code" is half of reproducibility. It is also what allows detecting an interrupted-and-forgotten run, and a configuration that changed in the middle of a collection.

### `reference` instead of `genome`

The original `genome` type intended to manage reference assets, including aligner indexes. That competes head-on with refgenie, which does it better.

Reduced to `reference`: it downloads FASTA, GTF/GFF3, transcripts and blacklist, and records **exactly which release**, with the detected chromosome convention. No building of indexes. It is 10% of refgenie and it is precisely the part that decides reproducibility.

### The chromosome convention is never converted silently

GENCODE's `chr1` against Ensembl's `1`. If genome and annotation diverge, quantification fails with no error at all — only wrong numbers.

`chromosome_naming` is first-class metadata, detected by sampling. A derivative whose parents diverge is `fatal`. Colligo warns and stops; converting automatically would be creating the silent error it is trying to avoid.

### Adoption through the channel the user already uses

The tool the community adopts works with one command and shows up where the user already looks. That is why three distribution decisions are first-class, not polish:

- **`check --samplesheet`** checks an nf-core pipeline's samplesheet against the content — files, pair, counts, repetition. nf-schema validates the columns of the sheet, not what the files contain.
- **MultiQC module** shows the result of `check` in the report that everybody already opens. Nobody needs to change habits to see it.
- **`colligo.lock`**, the `go.sum` of data: small, textual, versioned together with the analysis, checked at the start of the pipeline. The idea of a lockfile is not new — DVC has its `.dvc` files —; the content is: confirmed identities and counts, not just hashes.

### Declared against observed, beyond the isolated file

The same principle holds for study metadata:

- **Single-cell chemistry:** R1 length and barcodes against whitelists reveal the chemistry; a divergence from the declared one stops before alignment.
- **GEO supplementary matrix:** columns against the list of GSMs, gene ID namespace against the declared genome.
- **Duplicate across accessions:** identical content shows up for free because facts are indexed by root.

### Lovable, visibly fast, demonstrably reliable

Correct is not enough. A tool that is only more correct than the others is respected and little used. Three axes, each with concrete decisions.

**Lovable — solve something tedious in one command and explain what it found.**

- **`colligo get GSE116256`**: from accession to validated samplesheet. It fetches metadata, downloads, verifies in the same read, organises the samples by role and replicate, and delivers a `samplesheet.csv` ready for nf-core plus `colligo.lock`. It is a light-door command: no database; the resume state lives next to the files (`.part` and outboard). fetchngs covers part of this, but requires Nextflow; here it is one binary.
- **The error message is a diagnosis.** Every assertion that fails points to an entry of a diagnostics catalogue: what was observed, the probable cause, what to do. "R2 has 1.2 million fewer reads than ENA declares. Typical pattern of a truncated extraction. Redo it with `fasterq-dump` or download the FASTQ from ENA." The exit code still exists for machines; the person reads the sentence.
- **Zero configuration.** `colligo check .` discovers formats by content and pairs by name (`_R1`/`_R2`, `_1`/`_2`), and runs. TOML only when the user wants control.

**Visibly fast — parallelism that others do not have.**

- **Verification overlapped with the download.** The other tools download and then re-read the file to check the MD5. In Colligo the facts are computed while the bytes arrive; when the download ends, the verification has ended. Measurable goal: `get` with full verification costs at most 5% above the plain download.
- **Adaptive concurrency per server.** A fixed number of connections either underuses the server or gets blocked. Colligo adjusts by itself, like TCP congestion control: additive increase while throughput grows, multiplicative decrease at the first 429, 503, timeout or throughput drop. The limit configured per domain becomes a ceiling, not a value. The user does not need to guess `--workers`.

**Demonstrably reliable — proving that the tool itself does not lie.**

- **Crash at any point.** The suite kills the process at each write, `fsync` and `rename` of the resume and the database, and checks the invariant after each resumption: no file under its final name without having passed everything, no progress lost beyond the last group. Serious filesystem tools do this; bioinformatics ones almost never.
- **Formal model of resume and the state machine**, in TLA+, verified by model checking: no sequence of failures, restarts and server responses leads to `done` without checked content. The model is written before the Slice 3 code and stays in the repository.
- **Certificate verifiable by third parties.** `check --certificate` emits a JSON with root, size, facts, versions and assertions. A reference verifier of ~200 lines in Python, independent of the Go code, checks the certificate against the file without installing Colligo. Reliability becomes something another person checks, not something they need to believe — and the verifier is, as a bonus, one more independent implementation for M9.

### Speed has a physical floor, and the goal is to touch it

Validating plain gzip and FASTQ requires decompressing everything. The floor is decompression in pure Go; the goal is to stay within 10% of it, and in the first measurement the whole `check` came within 0.5%. `zcat`, in C, is ~20% faster than Go's inflate: that distance is reported, not chased, because closing it requires cgo or speculative decompression. Three decisions follow from that: the single read distributes the buffer in parallel, one goroutine per consumer, because `io.MultiWriter` would add up the costs; the parser operates on the buffer without allocating; and the fastest way to verify is not to verify again — facts by root turn the rerun into a `stat`. A quick mode exists (`check --quick`), but it never declares anything verified.

### A derived artifact has provenance, minimal

A GTF merged from GENCODE with Dfam was not downloaded, it was built. Without knowing which version of each parent went in, the result is not reproducible, and the absence is invisible.

`derived_files` and `derived_parents` record parents, builder, version and parameter hash. A derivative inherits the strictest classification of its parents — a derivative of a restricted parent is restricted.

Deliberately minimal scope. Inside a workflow, derivative provenance is the orchestrator's job: Nextflow's data lineage, experimental since 25.04, already records path, size and checksum of the outputs. Colligo covers the derivative built outside an orchestrator and, when it makes sense, reads the Nextflow record instead of competing with it.

### Portable manifest

The database is the canonical state, but a third party who receives your collection cannot verify anything without it. The manifest exports root and outboard, identities and custody state in a JSON that circulates.

In a restricted study the manifest comes out with opaque names and remains **fully functional** for attestation, because hashes are not sensitive. It is the mechanism that separates "internal reproducibility" from "verifiable reproducibility".

### Isolated FTP, HTTPS first, Aspera out of v1

We prefer HTTPS and inherit `Range`, retry and checksum from the main path. FTP stays isolated, with its own resume, only where there is no alternative. Aspera is an external binary with a licence and network configuration outside our control: out of v1.

### Idiomatic and polite concurrency

`errgroup` with `SetLimit`: an error cancels the siblings and all of them are waited for. Per-domain limits exist so as not to burn the institution's IP; raising them is the user's responsibility and is recorded in the config hash.

### Infrastructure

Latest stable Go (enables `iter.Seq`, which solves iteration over large studies without loading everything into memory). MIT licence. Module `github.com/<org>/colligo`.

Platform: the usage target is Linux (HPC, servers, WSL). Development also happens on Windows, and that has a consequence: `0600`/`0700` permissions, `verified_inode` and sync-folder detection assume POSIX. Decision: develop and test on WSL; the native Windows binary compiles, but only the light door is supported on it until a Windows CI exists. OneDrive is Windows's default synced folder — exactly the case `CONFIDENTIALITY.md` §5.3 wants to catch —, so a restricted study on native Windows is refused.

---

### Slice 1 decisions, reliability first

- **A blank line at the end of a FASTQ is `advisory`, not `blocking`.** It is harmless and common; blocking it generates a false alarm, and a false alarm pushes the user to `--permissive`, which is where a real problem gets through. It is counted in the fact and reported. A blank line *in the middle* remains `blocking`.
- **A `;` line in a FASTA is rejected.** It used to be counted as bases: a silently wrong count. Ignoring it could hide garbage. In an archaic format, failing closed costs little.
- **A false BSIZE in BGZF is rejected.** The `BC` subfield promises BGZF, and htslib, samtools and tabix depend on BSIZE to find the blocks: with it wrong, the inner gzip decompresses whole and the file still fails, or reads garbage, on random access. Validating only the EOF block let through a file that the rest of the ecosystem cannot read. The BSIZE chain must be contiguous from offset 0 to the exact end of the file, and the Python verifier checks it independently.
- **A UTF-8 BOM at the start is rejected in FASTQ, FASTA and GTF.** In the GTF it contaminated the first seqname: `﻿chr1` ≠ `chr1`, and the chromosome convention fell to `unknown` instead of `ucsc`. It is the silent error of M11, with no warning and wrong numbers further on. Removing the BOM silently would be converting without warning; the sniffer ignores it only so that the error is specific (`structure_invalid`, offset 0) and not a generic "unknown format".
- **The independent verifier distinguishes "I disagree" (1) from "I cannot judge" (2).** An unknown schema is "I don't know"; treating it as disagreement or agreement would be claiming something that was not verified. The same holds for a missing required field and for an unknown field: the verifier only says "I agree" about a certificate that it understands entirely, and a field it does not know may carry an assertion that it does not check. That is why growing the certificate is a `schema_version` change, not an optional field; and why the `AGREE` comes accompanied by the list of what was not recomputed, instead of letting the reader suppose that everything was.
- **A file-level `fatal` does not interrupt the other files; a tool-level `fatal` interrupts everything.** On the light door there is no state to save, and stopping at the first bad file would hide the diagnosis of the others. The exit code remains 2.
- **Output in English.** One language only on the public interface; the identifiers in `--json` are stable regardless of the text.
- **The performance goal measures what is ours.** See "Speed has a physical floor". Trading reliability for speed — cgo, speculative inflate — is left for later and only with measurement.
- **Versioned synthetic corpus, with the generator's truth.** Fixed bytes make certificates reproducible by third parties; the expected comes from whoever wrote the file, not from another parser, which catches the error that Go and the Python verifier would share.
- **The mate suffix of `fastq-dump --readids` only in the full form.** The FASTQ extractor (`fastq-4`) strips `.1`/`.2` from the end of the ID only when the ID has the form `<accession>.<spot>.<1|2>` with accession `SRR`, `ERR` or `DRR` (`SRR001666.1.1` and `SRR001666.1.2` become `SRR001666.1`). Stripping any trailing `.1` or `.2` would make `SRR1.1` and `SRR1.2`, which are different spots, become the same ID, and would hide an R2 that lost its first read. Without the normalisation, the `fastq-dump` pair gave a blocking `pair_ids` on an intact pair. `tools/verify_cert.py` implements the same rule by regular expression, written from the rule and not from the Go.
- **`check --quick` with no problem exits 5, not 0.** Code 0 is reserved for "complete and verified", and `colligo check --quick x && pipeline` must not proceed as if the file had been verified: a gzip truncated in the middle passes the quick check. A problem found by the quick check still exits 2 or 3. `validate` does not change, because it reads the whole file.
- **A malformed `--expected` is a usage error.** `verifier.ParseExpected` requires 32 hexadecimal characters for MD5 and 64 for SHA-256 and BLAKE3, case-insensitive, and the exit is 1 without reading any file. A malformed expected value is the typist's error; declaring it a fatal `checksum_mismatch` would accuse of corruption a file that nobody compared.
- **`Compute` memory bounded by test.** Heap peak ≤ 32 MiB above the baseline, measured by `runtime.ReadMemStats` over 2 GiB of gzip zeros (a ratio of ~1000:1) and over 20 million reads; allocations ≤ 8 and ≤ 16 KiB per MiB read. The ceiling comes from the structural floor (an 8 MiB ring) with ~3.5× headroom, and not from the observed peak, so as not to break on collector noise. A file that changes during the read is described by the bytes read: `Compute` does not compare `stat` before and after, because comparing `stat` is not verifying.
- **A file that changes during the read is `blocking`, and `stat` only invalidates.** `CheckFile` compares `fstat` before and after the read (size, mtime and ctime in nanoseconds, inode). If anything changed, `changed_during_read` invalidates the result; if nothing changed, `stat` proves nothing, and the `verified` state still comes only from reading the bytes. This respects "comparing `stat` is not verifying": here `stat` never approves. It is `blocking` and not `advisory` because an in-place overwrite can produce a hash of bytes that never coexisted on the disk, and a `verified` with that hash would be a false guarantee. It is not `fatal` because the file is not necessarily corrupted: running again fixes it. The remaining assertions are `skipped` and the hashes leave the report, because deriving conclusions from incoherent bytes only creates noise. `stat` also goes by the path, not only by the descriptor, and compares inode and device with the open file: the path is what the user and the certificate name, and the open descriptor keeps pointing to the old inode after a `rename` on top (as a safe download ends). Without this, the result would come out `verified` and `--certificate` would write, next to the path, a certificate of bytes that are no longer its own. A path that changed inode is `replaced`, a path that disappeared is `removed`; both give `changed_during_read`.
- **A missing mate is `advisory`, not `blocking`.** `pair_missing` warns that a file with a mate suffix does not have the other mate in the run, because Colligo cannot tell a forgotten pair apart from legitimate single-end data that carries `_R1` in the name. Blocking would break whoever has single-end data; staying silent would leave the M5 defence switched off without anybody knowing (pairing by name only, the same logic as `_R1_001` against `_R2_002`). With a single file in the run the warning does not fire: there is nothing to compare against.

## 4. Data types

Pluggable profiles over an agnostic core, plus `generic` as a fallback without a schema.

| Tag          | Type                                                | Main sources                  | Control         |
| ------------ | --------------------------------------------------- | ----------------------------- | --------------- |
| `rnaseq`     | Bulk RNA-seq                                        | GEO, ENA, dbGaP               | No              |
| `singlecell` | scRNA-seq (acquisition)                             | GEO, ENA                      | No              |
| `tabular`    | MAF, VCF, dose-response, clinical, signature matrix | cBioPortal, portals, adoption | No              |
| `reference`  | FASTA, GTF/GFF3, transcripts                        | GENCODE, Ensembl, Dfam        | N/A             |
| `structure`  | PDB, mmCIF, protein FASTA                           | RCSB PDB, adoption            | N/A             |
| `chipseq`    | ChIP-seq                                            | ENCODE, ENA, GEO              | Yes — Input/IgG |
| `generic`    | Unknown                                             | Any                           | No              |

`atacseq`, `methylation` and `wgs` left v1 for `BACKLOG.md`. Not because they are hard — the pattern is the same — but because no active project uses them, and a type built speculatively is a type not tested against reality.

`singlecell` is **acquisition only**: it validates library metadata (10x Chromium, Drop-seq, Smart-seq2, chemistry, layout). Validating the structure of single-cell output is out of scope — that belongs to the downstream pipeline. Robustness comes from doing less, well.

`tabular` is the lightest type and the one of most critical provenance. Files of KB to MB do not need an outboard or a worker pool — a cannon for a fly. But you need to know which version of the signature matrix you used and what date the portal export is from. Small-file path: a single hash, no outboard, full facts and audit. It is a mode, not a subsystem.

`role` (signal | control | reference) is more fundamental than sample_type — every experimental structure has "what I measure" against "my control".

Organising the metadata is the second value of the project, after reliability. Turning a GSE into a typed, validated sample sheet, with the control linked to the correct signal, is work that many people do by hand and get wrong. fetchngs generates a samplesheet for nf-core; what Colligo adds is validating that the declared control really is a control and that the experimental structure closes.

---

## 5. Layered metadata system

**Layer 1 — Universal core:** accession, source, role, layout, replicate, status, confidence, timestamps, classification.

**Layer 2 — Profiles:** valid roles, linking logic, required fields, validations. A custom profile can be registered.

**Layer 3 — Declaration:** the TOML. The profile expands the minimal declaration into a complete contract.

**Layer 4 — Confidence:** each piece of metadata carries an origin — `declared` (explicit from the API), `inferred` (heuristic, raises an issue), `confirmed` (the user validated). Exported as `QC_Status`.

Validation in two phases: local synchronous (fields, types, accession format — cheap, stops before the network) and remote asynchronous in fan-out (does the declared type match? do the accessions exist? is the set of files the same as before?).

---

## 6. Relationship with Scribo

Colligo guarantees the inputs before the pipeline; Scribo observes the results after; each side corrects the other. The integration is by contract (`CONTRACT.md`, an identical copy in both repositories): opening the database, `tool_runs`, and who owns which table.

**What Colligo gives Scribo:** sample identity (`samples`), verified roots (`files`), facts for input × result invariants (`facts`), accession status at the source, study classification. With that, Scribo links each value to the inputs it depends on, refuses a result that contradicts the input — more mapped reads than the FASTQ has —, and lists the stale results when an input is invalidated.

**What Scribo gives Colligo:**

- **Evidence after the pipeline.** Colligo does not align reads, by decision. A sample with an alignment rate far from the others suggests a swapped organism or a wrong file; Scribo detects the deviation (robust z-score) and records a `suspect` signal. Colligo consumes it as one more assertion, without crossing the boundary of the analysis.
- **Identity confirmed by genotype.** Output of somalier or NGSCheckMate, compared with the declared, becomes `identity_confirmed` or `identity_mismatch` in custody.
- **Reprocessing plan.** When `recheck --upstream` invalidates an input, Scribo knows which results and stages depended on it and generates the samplesheet with only the affected samples.
- **End-to-end completeness.** `status` comes to show the funnel up to "processed".
- **Scrub priority and `gc` with provenance**, from `result_inputs`.
- **Corpus of real failures**, from the signals and violations recorded.

**Shape:** proposal of one module with two binaries and a common core (`facts`, `runs`, `store/open`), decided in Scribo's Slice 0. Until then, the contract guarantees the conversation without sharing code.

---

## 7. Declared limits

Stated clearly so as not to create false confidence:

- It does not encrypt anything at rest. Use the system's disk encryption.
- It does not anonymise file content. A FASTQ is identifiable by nature.
- It does not replace access approval. Colligo does not know whether you have the right to the data.
- It does not protect against the downstream pipeline. If the next tool writes paths to a log, the problem comes back.
- It does not do analysis, and the boundary is deliberate: we validate that the file is structurally what it says it is, never that the content makes biological sense. Rule of thumb: no check that requires aligning reads. Strandedness, sex, sample swap by genotype and fine contamination belong to the pipeline.
- **It does not orchestrate or supervise execution.** That belongs to Nextflow or Snakemake, which already run each task in its own directory publishing output only on success, record command, versions and checksums, measure resources and retry. A command wrapper, failure triage and a job watchdog were considered and discarded for that reason. A pipeline that uses Colligo plus an orchestrator is the intended case, not a concession.
