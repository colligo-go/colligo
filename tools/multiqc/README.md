# multiqc-colligo

A [MultiQC](https://multiqc.info) module that shows the report of `colligo check --json`: one table of files, the problems Colligo found, what it did not check, and, when `--source` was used, the comparison with what the source declares.

It reads `schema_version` 2 of the `check` report. It is a plugin for now; see "Plugin now, MultiQC later" below.

## Install

```sh
python -m venv .venv
.venv/bin/pip install ./tools/multiqc
```

It needs Python 3.10 or newer and MultiQC 1.30 or newer (below 2). It is tested on MultiQC 1.30 and on the latest 1.x.

## Use

Save the JSON of each check as `<name>.colligo.json`, then run MultiQC on the folder:

```sh
colligo check --json --permissive sample_R1.fastq.gz sample_R2.fastq.gz > sample.colligo.json
multiqc .
```

The `colligo/check` module of `packaging/nf-core/` writes exactly these files (`*.colligo.json`). Use `ext.args = '--permissive'` there so that a failed check does not stop the pipeline before MultiQC runs; the report still carries the failure.

MultiQC finds a file by its name (`*.colligo.json`) and the module then checks the content: `schema_version`, a `files` list and an `exit_code`. A file with that name that is not a Colligo check report, or has another `schema_version`, is not used. It is not dropped in silence: the Colligo section lists it with the reason, and MultiQC logs a warning.

The module runs before the modules of `module_order`. To put it elsewhere, list `colligo` in your MultiQC config.

## What the report shows

- **General Statistics**: three columns per Colligo report: the result (`✓ verified`, `! advisory`, `✗ failed`, `– not verified`), the source the files were compared with (`ena:DRR021374`, or `– not compared` when only the files themselves were checked), and the number of blocking problems, each counted once.
- **Files**: one row per file with the status Colligo gave it, the exit code of the report, the result of the whole report, the number of reads and bases Colligo counted in it, and how many advisory and not-checked assertions the file has.
- **Problems**: each failed assertion once, with what Colligo observed, the likely cause (marked as a hypothesis) and what to do, and the files or runs it applies to. A failed run is one problem, not one per file.
- **Not checked**: every comparison Colligo did not make and why, including a missing `--source`.
- **Source: ACCESSION**, one section per report that has `source`: for each run, the declared and observed counts, the result of each comparison, the files the source lists, the local file that stands for each, and whether its MD5 was compared: `✓ matched`, `✗ differs`, or `– not compared` (a decompressed copy can never match the MD5 published for the compressed file, so it is also listed under Not checked; the MD5 of the source is shown, never as a match).

### What counts as verified

The page follows Colligo: a symbol and a word always go together, and colour is never the only signal.

| Shown | Meaning |
| --- | --- |
| `✓ verified` | Colligo verified the files and nothing it compared failed. `verified, with gaps` means some comparison was skipped or the source declared no value for it; the gaps are listed under Not checked. |
| `! advisory` | Verified, with an advisory finding such as `layout_mismatch`. |
| `✗ failed` | A blocking finding, an `invalid` or `unreadable` file. A report made with `--permissive` exits 0 but keeps this result. |
| `– not verified` | `valid` (`colligo validate`, structure only), `quick` (`check --quick`), or a report with no file at all. Never shown as OK. |
| `– unknown` | A status or severity this module does not know, or an exit code that nothing in the report explains. Never shown as OK. |

A skipped assertion, a count the source does not declare (`not declared`) and a comparison that was not made are shown as `– not checked`, never as passed. A report is `verified` only if its exit code is 0 and every file and every comparison agrees.

### Limits

- **Reads** and **Bases** are the `reads` and `bases` of each file in the report: what Colligo counted in the one read of a FASTQ. They are blank (never 0) for a file that was not read whole (`--quick`), for FASTA and GTF, and for a file that was unreadable or invalid. A report written before the report carried them shows, for paired files only, the count of the pair check (`pair_count`) as Reads, and no Bases.
- MultiQC skips files larger than its `log_filesize_limit` (50 MB by default).

## Plugin now, MultiQC later

A module inside MultiQC is released with MultiQC, so every change of Colligo's report would wait for a MultiQC release. Colligo is at `v0.1.0`, its report is at `schema_version` 2, and the bioconda recipe and the nf-core module are not submitted yet (`packaging/README.md`). As a plugin, the module follows the report format release by release and users can try it now. When Colligo is on bioconda and the format has settled, the same code can be offered to MultiQC as a pull request. That is the maintainer's decision, and nothing has been opened there.

## Tests

```sh
python -m venv .venv
.venv/bin/pip install -e './tools/multiqc[test]'
.venv/bin/python -m pytest tools/multiqc
```

The examples in `tests/examples/` are reports written by the current `colligo`, on the synthetic data of `testdata/synthetic/` and, for `--source`, on the recorded ENA responses of `source/ena/testdata/`, with no machine path in them (the Go golden files use `<dir>`). Seven of them are byte for byte the golden files of `cmd/colligo/testdata/golden/json/`, and a test fails when either side changes, so a change of the report in Go cannot leave this module reading an old shape. The other three were written with:

```sh
colligo check --quick --json testdata/synthetic/plain.fastq > tests/examples/quick.colligo.json
colligo validate --json testdata/synthetic/plain.fastq > tests/examples/validate.colligo.json
colligo check --json --permissive testdata/synthetic/truncated.fastq.gz > tests/examples/permissive-invalid.colligo.json
```

| Example | What it shows |
| --- | --- |
| `verified` | a pair with no `--source`, verified |
| `source-verified`, `source-link` | `--source` agrees; the second took the accession from a link |
| `blocking` | `--source` and a shortened read 2 (`count_mismatch`, exit 3) |
| `advisory` | `layout_mismatch`, a run labelled single whose files form a pair |
| `not-declared` | the source left `read_count` empty |
| `unreadable` | a file that could not be read (exit 2) |
| `quick`, `validate` | never `verified` |
| `permissive-invalid` | a truncated gzip under `--permissive`: exit 0, file `invalid` |

The data directory gets two flat tables: `multiqc_colligo` (one row per report) and `multiqc_colligo_files` (one row per file).

The end-to-end tests run the real `multiqc` on these examples and read the HTML and the General Statistics table it writes.
