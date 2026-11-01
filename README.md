# Colligo

Do I have the data I think I have?

Colligo identifies, verifies and watches genomic data from public and private sources.
Downloading with an MD5 is a solved problem; knowing what the file *means* is not.

```
colligo check SRR2133898_1.fastq.gz SRR2133898_2.fastq.gz
colligo check . --certificate
colligo check --source ena:SRR10663037 downloads/
colligo check --source ena downloads/                  # the accession is deduced from the file names
colligo check --source https://www.ebi.ac.uk/ena/browser/view/DRR021374 .
find . -name '*.fastq.gz' | colligo check --files-from -
colligo identify genome.fa.gz
```

Colligo helps to read what you type, and says so: an accession taken from a link or deduced from the file names is shown in the report (`Input: accession DRR021374, deduced from the file names`), two runs in one folder are a usage error that lists both instead of a guess, and `colligo check` with no path suggests `colligo check .` instead of checking another folder in silence. A file that is a common mistake (an SRA archive, a BAM, a ZIP, a spreadsheet given as a samplesheet) is named, with what to do. An unknown flag gets "did you mean", and `-h` of each command shows examples and the exit codes.

A failure is stated as a failure, in the first line, with its exit code. This is what `colligo check --source ena:DRR021374 .` prints when read 2 was shortened, a file that is still valid FASTQ:

```
✗ FAILED  DRR021374 does not match ENA: do not use these files  exit 3

Run DRR021374  PAIRED · ENA status public
  Bases    3,454,158 declared by ENA, 1,747,050 observed (-49.4%)
  ✗ count_mismatch · blocking · M15
    Problem  The content has 1747050 bases over the 2 file(s) of run DRR021374, but 3454158 were expected.
    Likely   (hypothesis) The content is valid but shorter or longer than the source declared, as with an extraction that shortened the reads.
    Do       Extract or download the file again and compare the counts with the source record.
  File                Reads      Bases  Bases/read  Result
  DRR021374_1.fastq  13,767  1,733,283       125.9  ✗ invalid (its run failed)
  DRR021374_2.fastq  13,767     13,767         1.0  ✗ invalid (its run failed)

Checked
  ✓ format and structure of 2 files
  ✓ the mates among 2 files agree in read count and read names
  ✓ ENA lists 1 run as public
  ✗ bases against what ENA declares: 0 of 1 run pass

Not checked
  – MD5 and size of 2 extracted files: not the published .gz, so they cannot match its MD5
  – read count of run DRR021374: skipped, because count_mismatch failed

What to do
  1. Extract or download the file again and compare the counts with the source record.

2 files: 2 invalid · 1 run: 1 fail · 4.1 MB · 0.4 s

Explain
  colligo explain count_mismatch
```

`--quiet` prints only the first line, `--verbose` adds the skipped files and every assertion that passed, `--color=auto|always|never` and `NO_COLOR` control the colour, and `colligo explain count_mismatch` says in plain words what a diagnosis means and how to confirm it. `--json` is the same document whatever the styling.

Exit codes: 0 verified, 1 usage error, 2 fatal, 3 blocking problem pending, 4 interrupted, 5 quick check found nothing in what it read (content not read in full).

- **identify** — which release, assembly and sequence collection this file actually is, from its content
- **verify** — it is whole, and it is what the source and you declared
- **watch** — it still holds, on disk and at the source

Status: early development. `v0.2.0` closes slice 2: `check` compares the files with what ENA declares (`--source`) and checks nf-core samplesheets, and `identify` names a FASTA or a GTF by its content and by official checksums; slice 1 (`v0.1.0`) was the light door, `check` and `validate`. While the major version is 0, any minor release may change the interface. See `CHANGELOG.md` and `docs/ROADMAP.md`.

License: MIT.
