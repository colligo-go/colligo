# Colligo — Backlog

Features that were consciously postponed. **Do not implement anything from here without an explicit request.**

These left the backlog and went to `ROADMAP.md`: `compat` (Slice 6, on top of Sequence Collections — refcompat already exists, in Python and experimental, and so does GATK's CompareReferences, also experimental; the gap now is a fast binary integrated with file identity, not the idea), the validator as a separate door (it became the light door, Slice 1), the nf-core module and bioconda recipe (sketched in Slice 2), and the positioning against prior art (it became `RELATED.md`, written now and not at publication time).

Rule: every item carries a sentence on "what would unlock it". Without it, the item is a whim; with it, it is a postponed decision with a trigger.

---

## Usability for people who do not use a terminal

Most biologists will not run a command line tool, however polished. Colligo should reach them where they already work, on top of the modular text output of Slice 2 (SPEC §16, "Text output"):

- A self-contained HTML report: one more renderer of `render/`, opened in a browser, with the same blocks as the text report.
- A Galaxy tool wrapper, so that `check` and `identify` are a form and a button.
- A quickstart for biologists: three commands with screenshots, a glossary (what *blocking* and *advisory* mean) and common situations ("I downloaded with fastq-dump", "I renamed my files").
- Usability tests: three tasks for three to five lab users, watched; they decide the order of the rest.
- Small touches of the text report: a link from each diagnosis id to its documentation on terminals that support it, a ready rerun command after a failure, and an ASCII theme chosen automatically when the terminal has no Unicode.

Bioconda and static binaries in the GitHub releases are already planned (`packaging/`).

Unlocks when: `v0.2.0` is released with the polished text output and the first users outside the project try it.

---

## Translated diagnostics

Sentences of the `diagnoses/` catalogue in Portuguese and other languages, chosen by locale.

Unlocks when: a user needs the sentence in their own language; the identifiers in `--json` are already stable and do not depend on the text.

---

## `colligo gc` — a garbage collector that understands provenance

Answer "these 8TB can die because the recipe is recorded and redoing it costs X hours".

Every lab has a scratch area with tens of terabytes that nobody dares to delete, because nobody knows what is regenerable.

Status: nothing good exists. Snakemake has `temporary()`, but only inside a workflow you control and whose rule you have not edited since. HPC centres purge by age — an administrator's tool, which does not understand what the file is. The question about "a CCleaner for bioinformatics" circulates in the community without an answer.

Unlocks when: there are a few months of real Scribo `result_inputs`, which is the usage history that was missing — a file that no result uses is a candidate; a file whose result can be redone from inputs under custody is regenerable; the rest is irreplaceable. `derived_files` complements this for derivatives built outside a pipeline.

Note: this is the biggest genuine gap on the list — the item where "nobody has this" is most literally true.

---

## `colligo scan` — de-identification before sharing

Before uploading a VCF to a public repository: does it contain identifying variants? Does the BAM header have a patient name? Does the file name have a medical record number?

Status: thin tooling, high risk. It connects directly with the regime of `CONFIDENTIALITY.md`, and is its outbound counterpart — today the document protects the data that comes in, not the data that goes out.

Unlocks when: derived data from a restricted source is actually shared. Not before: a badly calibrated scanner gives false confidence, which is worse than no scanner.

---

## Propagated sample identity

A token that travels with the sample through all the stages and is checked at each one, to catch the glob that shuffled files and made sample A's FASTQ become sample B's BAM.

Status: genetic tools (somalier, NGSCheckMate) catch a swap after the fact, by comparing the data, and only for genomic data. Nobody catches it at the plumbing level, which is where the error happens.

Chosen path: not a token in the plumbing, but Scribo importing the output of somalier or NGSCheckMate and returning `identity_confirmed` or `identity_mismatch` to custody (`CONTRACT.md` §4). It catches the swap after the fact, but with tools that already exist and without changing the pipeline.

Unlocks when: Scribo's Slice 4 is ready. Nothing to implement in Colligo beyond consuming the signal (SPEC §14.1).

---

## Semantic diff of pipeline outputs

Recorded without a trigger, on purpose.

You upgrade STAR from 2.7.10 to 2.7.11 and rerun. Did the results change? Did they change in a way that matters? Today that is `md5sum` (which always differs, because of a timestamp in a header) or looking at a plot and hoping.

It is not an extension of Colligo — it is a separate, large project, with format-specific logic (BAM, VCF, count matrix, peak set), separating version noise from a genuinely different result.

It is the one of highest scientific value among everything considered, and the only real candidate for a paper besides Colligo itself. It stays here as a recorded idea, not as a postponed item: it has no trigger because it is not a postponed decision, it is another project.

---


## Types `atacseq`, `methylation`, `wgs`

Profiles and schemas of the three types that left v1.

Status: the pattern has been validated by the previous slices. Each one is a repetition — define the columns of `*_samples` when implementing, not before.

Unlocks when: an active project needs one of them. A type built speculatively is a type not tested against reality.

---

## Granular repair

A command that takes an already complete file, revalidates it block by block and downloads again only the corrupted ones via `Range`.

Status: the foundation already exists. The `file_groups` table, the Bao outboard and ranged download are built for resume in Slice 3, and `attest` in Slice 4 already identifies the diverging groups. Only the trigger for the command is not wired.

Unlocks when: the first real case of a corrupted file appears in production and the full re-download becomes expensive enough to justify surgical repair.

---

## Content resync (rolling hash)

For the case of corruption that shifts bytes, resynchronise the groups by content, rsync-style, instead of re-downloading from the point of the error to the end.

Unlocks when: the shift case proves common enough to justify the complexity. Today it is rare and the current behaviour is correct, just not optimal.

---

## Aspera (ascp)

Transfer via the Aspera protocol, an order-of-magnitude gain on large transcontinental FASTQ.

Unlocks when: there is real, recurring demand, and the user has `ascp` configured. It is an external binary with a licence and network configuration outside our control — a high support cost. Note: for dbGaP data, `adopt` covers the case without needing the protocol.

---

## Tested native Windows support

Current decision: development and testing on Linux or WSL; native Windows compiles and supports only the light door; a restricted study on native Windows is refused (`CONFIDENTIALITY.md` §5.2). This item is full support for the heavy door on native Windows.

Unlocks when: there is a real user on native Windows and a stable Windows CI.

---

## PostgreSQL Store

An alternative implementation of the `Store` interface for consortium scale.

Status: the `Store` interface has been an abstraction from the start, and `storetest` is the gate any alternative implementation must pass. Keep the interface narrow, but do not invest speculatively.

Unlocks when: a real case exceeds what SQLite comfortably handles AND there is a need for concurrent multi-process writes.

---



## Data types — v2 and beyond

Candidates: TCGA/GDC, GSA (BGI), CUT&RUN, Hi-C, structure validation of single-cell output, and those that v1 reveals.

GDC requires a token and controlled files; GSA has a poorly documented, unstable API — both deserve dedicated planning, and both are natural candidates for `adopt` instead of an adapter.

Unlocks when: v1 is stable and real use shows which are missing. Do not decide the 5 of v2 on paper now.

---

## Speculative metadata prefetch

Optimisation: fetch metadata of likely experiments before the user asks.

Unlocks when: profiling shows that metadata fetch latency is a real bottleneck. Unlikely given the parallel fan-out already planned.

---

## Reading Nextflow data lineage

Import Nextflow `lid://` records as `derived_files`, and later verify the outputs it recorded. Nextflow's lineage, experimental since 25.04, records path, size and checksum, but has no command for later verification.

Unlocks when: lineage leaves experimental status and there is real use of Colligo together with Nextflow. Reproducing Nextflow's checksum algorithm is a prerequisite to investigate.

---

## Declared × observed organism

A MinHash sketch of a few thousand reads compared with small reference sketches answers "is this really human?". It catches a sample labelled with the wrong organism and gross contamination, before any alignment. sourmash and Mash already do the sketch; what is new is using it as an assertion against the metadata.

Alternative already covered: Scribo's `suspect` signal, from deviation in alignment rate between samples, catches the same error after the pipeline, without sketches and without Colligo crossing the boundary of the analysis. MinHash remains useful for catching the error **before** alignment.

Unlocks when: there is a real case of a swapped organism that the post-pipeline signal caught too late, and a way to distribute the reference sketches without bloating the light-door binary.

---

## Rust accelerator for the extractor

A Rust implementation of the fact extractor, as a separate process, through the contract of SPEC §9 "Substitution boundary". Libdeflate or zlib-ng for decompression, a FASTQ parser over the buffer, the same facts byte for byte as the Go extractor.

Rules, fixed now so that the architecture is born with the door open:

- A separate process. Neither cgo nor WASM.
- Optional. Nothing breaks without it.
- One boundary: the extractor. Network, database, adapters and CLI stay in Go only.
- One direction: Go calls Rust.
- One contract: the fact schema with a shared `extractor_version`.
- It enters only with a differential test and fuzzing against the Go extractor.

Unlocks when: the benchmark of `TESTING.md` §7.1 shows, after the switch to `klauspost/compress` and the parallel distribution of the consumers, that a full `check` is still above the 110% target of the floor because of decompression.

Note: it also pays off as a second, independent implementation, the defence of `RELIABILITY.md` M9.

---

## Parallel decompression of plain gzip

Plain gzip has no entry point in the middle of the stream, so it is not trivially parallelisable. rapidgzip shows that speculative parallel decompression works and reaches several times the speed of one core.

Unlocks when: the benchmark of `TESTING.md` §7.1 shows that plain gzip decompression is the dominant bottleneck in real use, after the library switch and the parallel distribution of the consumers have been exhausted.

---

## Encryption at rest

Wrap the database and the files with a managed key.

Status: out of scope by decision, documented in `CONFIDENTIALITY.md` §7. Operating-system disk encryption solves the same problem better and is not our responsibility.

Unlocks when: there is an explicit regulatory requirement that disk encryption does not satisfy.
