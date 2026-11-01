# Validated on real data

Colligo (branch of slice 2.10) was run with `check --source ena:<run>` on 28 small Illumina runs from the public ENA, downloaded on 2026-11-01. Reads, bases and MD5 of every file were counted again with a Python script using only the standard library `gzip` and `hashlib`; MD5 was compared with the one the ENA publishes. Nothing disagreed.

| Run | ENA label | Files | Exit | Colligo reads/bases = independent | MD5 = ENA | Note |
|---|---|---|---|---|---|---|
| DRR015103 | PAIRED | 2 | 0 | yes | yes | verified |
| DRR021374 | PAIRED | 2 | 0 | yes | yes | verified |
| DRR027355 | SINGLE | 1 | 0 | yes | yes | verified |
| DRR037523 | PAIRED | 1 | 0 | yes | yes | verified |
| DRR037776 | SINGLE | 1 | 0 | yes | yes | verified |
| DRR037777 | SINGLE | 1 | 0 | yes | yes | verified |
| DRR049115 | PAIRED | 2 | 0 | yes | yes | verified |
| DRR052118 | PAIRED | 2 | 0 | yes | yes | verified |
| ERR12459081 | PAIRED | 2 | 0 | yes | yes | verified |
| ERR12476880 | PAIRED | 2 | 0 | yes | yes | verified |
| ERR12477684 | PAIRED | 2 | 0 | yes | yes | verified |
| ERR5060520 | SINGLE | 2 | 0 | yes | yes | layout_mismatch |
| ERR5514981 | SINGLE | 3 | 0 | yes | yes | layout_mismatch |
| SRR10603344 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR10663037 | PAIRED | 3 | 0 | yes | yes | verified |
| SRR11039629 | SINGLE | 1 | 0 | yes | yes | verified |
| SRR11621881 | SINGLE | 1 | 0 | yes | yes | verified |
| SRR11772037 | SINGLE | 1 | 0 | yes | yes | verified |
| SRR11822016 | PAIRED | 3 | 0 | yes | yes | verified |
| SRR12334939 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR12348193 | PAIRED | 3 | 0 | yes | yes | verified |
| SRR12404235 | PAIRED | 1 | 0 | yes | yes | verified |
| SRR19804457 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR21389762 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR27469745 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR27469746 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR31016516 | PAIRED | 2 | 0 | yes | yes | verified |
| SRR8478468 | PAIRED | 2 | 0 | yes | yes | verified |

`ERR5060520` and `ERR5514981` are labelled SINGLE by the ENA but hold a pair; both get the advisory `layout_mismatch` and exit 0. Every `read_count` was explained by one of the three readings, so no new fixture was needed.

## Real uses

| Use | Result |
|---|---|
| ENA link copied from the browser (`--source https://www.ebi.ac.uk/ena/browser/view/DRR021374`) | verified, `accession_from: link`, exit 0 |
| `--source ena` with no accession, on `ERR12477684` files | verified, `accession_from: deduced`, exit 0 |
| `--files-from` (a file and standard input) | verified, exit 0 |
| R1 of `DRR021374` renamed | linked by MD5 (`linked_by: md5`), verified, exit 0 |
| `DRR021374` decompressed, R2 cut to 1 base per read, with `--source` | `count_mismatch`, blocking, exit 3 |
| the same copy without `--source` | passes the pair checks; the advisory `mate_collapsed` flags R2, exit 0 |
| a folder with one `.sra` (a stub with the `NCBI.sra` signature, not a real archive) | `input_sra`, blocking, exit 3 |
| Ctrl-C during `check --source --cross-check --certificate` on a 700 MB file | exit 4, no output file written |
| `--samplesheet` with 3 correct pairs / with two R2 swapped | exit 0 / `pair_count` and `sheet_mates_swapped`, exit 3 |

## GENCODE

`gencode.v50.primary_assembly.annotation.gtf.gz` and `GRCh38.primary_assembly.genome.fa.gz` of human release 50 were downloaded on 2026-11-01. Both MD5 values equal the ones in the official `MD5SUMS`, and `colligo identify` reports both as `identified` by checksum. The seqcol digest of the FASTA, `tmnbiAyj2fke68d_TYjq2g487US8C15r` (194 sequences), is served by the reference server `seqcolapi.databio.org`, whose names, lengths and sequences attributes equal the ones Colligo computed. It is the first entry of `identity.Known`.

## MultiQC

The plugin was run on the `--json` of real runs: verified with and without `--source`, a renamed file, the advisory (`ERR5060520`, and the collapsed mate without a source) and the blocking case with `--permissive`. Only verified reports show `verified`; the advisory and the failed ones show their own state, the failed one also when its exit code is 0. The MD5 check column reads `not compared` for the decompressed copies and never `matched`. Reads and Bases appear for each file.

## Not covered

No run with a real index-read file was found in the ENA, so index reads are not measured. The `.sra` case used a stub, not a downloaded archive.
