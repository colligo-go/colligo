# Colligo — Related work

What already exists, what each tool solves, and where Colligo fits. Written early on purpose: honest positioning forces scope cuts, and it is better to cut before implementing.

Rule of this document: every claim of "nobody does X" carries a date and is revisited. The field moves; `compat` was once "nobody has it" and stopped being so.

Last review: 2026-09.

---

## 1. The thesis in one line

The tools below guarantee **bytes** or **transfer**. Colligo guarantees that the file is **what the source and you declared** — structure, counts, pair, identity, completeness and status at the source — and follows this over time. No single piece is new. The combination, with this focus, is.

---

## 2. Downloading public data

| Tool                                   | What it does                                                                                        | What it does not do                                                                     |
| -------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| iSeq                                   | GSA, SRA, ENA, DDBJ, GEO; checks size and MD5; resumes downloads. Published in Bioinformatics, 2024 | Does not validate structure, declared count, pair or identity; does not watch over time |
| nf-core/fetchngs                       | Metadata and FASTQ from SRA/ENA/DDBJ/GEO; generates a samplesheet for nf-core pipelines             | Same; custody ends at the end of the workflow                                           |
| ffq, pysradb, GEOfetch                 | Metadata and file links                                                                             | Do not download with custody                                                            |
| enaBrowserTools, fastq-dl, kingfisher  | Download from ENA/SRA over various protocols                                                        | Same                                                                                    |
| q2-fondue                              | Acquisition inside QIIME 2                                                                          | Restricted to that ecosystem                                                            |
| sra-tools (`prefetch`, `fasterq-dump`) | Official transfer and extraction                                                                    | Extraction can come out truncated with exit code 0 (the QC Fail case)                   |

Consequence for Colligo: download is not a differentiator. `fetch` exists because custody must start at the first byte, not to compete with iSeq.

---

## 3. Byte integrity over time

| Tool                 | What it does                                                                                                  | What it does not do                                                              |
| -------------------- | ------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| git-annex            | `fsck --incremental --incremental-schedule` verifies checksums in an amortised way over weeks; detects bitrot | Does not know what a FASTQ, GTF or pair is; does not know bioinformatics sources |
| DataLad              | Versioned datasets on top of git-annex; `download-url` records the origin; standard in neuroimaging           | Same; provenance is of the URL, not of semantics                                 |
| DVC                  | `import-url` records an ETag or checksum; `update` detects that the source changed                            | Same                                                                             |
| ZFS, Btrfs, SnapRAID | Per-block scrub at the filesystem level                                                                       | Below the application; no notion of a logical file or of a source                |

Consequence: "amortised reverification with block-level localisation" is **not** Colligo's contribution. The BLAKE3 tree is infrastructure, not thesis. Whoever already uses DataLad can keep using it; Colligo adds the semantic layer that DataLad lacks.

---

## 4. Format validation

| Tool                                                                             | What it does                     |
| -------------------------------------------------------------------------------- | -------------------------------- |
| `samtools quickcheck`                                                            | Checks the EOF block of BAM/CRAM |
| `gzip -t`, `bgzip`                                                               | gzip integrity                   |
| FASTQ validators (fastQValidator, fqtools and others; list in the FAIR Cookbook) | FASTQ structure                  |
| `gffread`, `gt gff3validator`                                                    | GTF/GFF3                         |
| `vcf-validator`                                                                  | VCF                              |

Scattered across different languages and interfaces, and each one looks at an isolated file, without what the source declared. Colligo uses the same read for structure, count and hash, and compares against the source.

---

## 5. Reference identity and compatibility

| Tool                                | What it does                                                                                     | Status                                                         |
| ----------------------------------- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------- |
| refget Sequence Collections (GA4GH) | Standard of content-derived identifiers for sequence collections; comparison between collections | Approved standard; reference implementation in Python          |
| refgenie                            | Manages reference assets and indexes                                                             | Mature; Colligo does not compete — it only records the release |
| refcompat                           | Compatibility between FASTA, `.fai`, `.dict`, BAM/CRAM, VCF, GTF, BED via refget/SeqCol          | Python, experimental, no release                               |
| GATK CompareReferences              | Compares reference FASTAs                                                                        | Experimental                                                   |

Consequence: `compat` is not a novel idea. What Colligo offers is a fast binary in which compatibility is a query over facts already extracted (`seqcol`, `coord_digest`), integrated with the identity and custody of the files. `identify` — saying which release an orphan GTF is, from its content — has no known equivalent.

---

## 6. Execution provenance

| Tool                  | What it does                                                                                                | What it does not do                                                                  |
| --------------------- | ----------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Nextflow data lineage | Since 25.04, experimental: records path, size and checksum of outputs; `lineage list/view/find/render/diff` | Has no later verification command; does not cover data acquired outside the workflow |
| Snakemake             | Provenance and `temporary()` inside the workflow                                                            | Same                                                                                 |

Consequence: `derived_files` stays minimal. Inside a workflow, provenance belongs to the orchestrator. Reading Nextflow's lineage is in `BACKLOG.md`.

---

## 7. Adoption and performance

| Tool           | Relationship with Colligo                                                                                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| nf-schema      | Validates the columns of an nf-core samplesheet against a JSON schema; does not look at the content of the files. `check --samplesheet` completes it                      |
| MultiQC        | Aggregates QC reports; Colligo enters as a module instead of creating its own report                                                                                      |
| Cell Ranger    | Detects the 10x chemistry by itself, inside the run; Colligo compares the observed chemistry with the declared one before the pipeline                                    |
| sourmash, Mash | MinHash sketches; a possible basis for declared × observed organism (backlog)                                                                                             |
| DVC            | `.dvc` files work as a lockfile of bytes; `colligo.lock` pins identities and counts                                                                                       |
| rapidgzip      | Parallel decompression of plain gzip; reference for the performance ceiling (backlog)                                                                                     |
| aria2          | Download with several connections per file; fixed number of connections and verification after the download. Colligo overlaps the verification and adapts the concurrency |
| TLA+, TLC      | Specification and model checking of the resume protocol; a common technique in distributed systems and rare in bioinformatics tools                                       |

---

## 8. nf-core is a channel, not a competitor

nf-core is a community of Nextflow pipelines; fetchngs is a workflow that orchestrates other tools. Colligo is a binary that runs on its own or inside any pipeline. The categories are different, and the desired relationship is one of composition: Colligo as an nf-core module called before alignment, and `check --samplesheet` validating the input that nf-core pipelines already receive.

Language is not what separates the two things. Go eases distribution — static binary, trivial biocontainer, instant start-up — but an isolated Colligo check can be rewritten in any language inside a workflow. What cannot be rewritten quickly is the architecture of facts by root and the mutant benchmark.

| Language | Where it wins                                                        | Where it loses to this project                                  |
| -------- | -------------------------------------------------------------------- | --------------------------------------------------------------- |
| Go       | network, concurrency, static binary, development speed               | decompression without SIMD; smaller format ecosystem, weak CRAM |
| Rust     | decompression and parsing (libdeflate, zlib-ng, noodles, needletail) | async for network, heavier build, slower development            |
| C/C++    | performance ceiling (htslib, rapidgzip)                              | memory safety, distribution, maintenance by one person          |
| Python   | ecosystem, where fetchngs and refcompat live                         | speed on the hot path, distribution outside conda               |

Decision: Go for the product, with a narrow door to an optional Rust extractor (`DESIGN.md`, "Language").

---

## 9. What nobody covers together

As of this review, no tool is known that does:

- Compare the `read_count` and `base_count` published by the source against the content of the FASTQ (M15).
- Re-query the source and warn that a run in use was suppressed as incorrect (M16).
- Identify the release of a GTF and the sequence collection of a FASTA from the content, with one command.
- Do all of this in a single read per file, without a database, with an exit code that fails closed.
- Measure, in a public mutant benchmark, which silent failures each tool lets through.
- Compare observed single-cell chemistry, and supplementary matrix columns, against the GEO metadata before the pipeline.

This is the list that Colligo needs to prove, and this is the list that the benchmark of `TESTING.md` §7 measures.

---

## References

- iSeq — github.com/BioOmics/iSeq; Bioinformatics 40(11), btae641, 2024
- git-annex fsck — git-annex.branchable.com/git-annex-fsck
- DataLad download-url — docs.datalad.org
- DVC import-url and update — doc.dvc.org/command-reference
- Refget Sequence Collections v1.0.0 — ga4gh.github.io/refget/seqcols
- refcompat — github.com/GregRR/refcompat
- GATK CompareReferences — gatk.broadinstitute.org
- Nextflow data lineage — docs.seqera.io/nextflow/tutorials/data-lineage
- ENA, suppression policy — ena-docs.readthedocs.io/en/latest/faq/release.html
- QC Fail, corruption in SRA extraction — sequencing.qcfail.com
- FAIR Cookbook, FASTQ validators — fairplus.github.io/the-fair-cookbook
