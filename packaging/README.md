# Packaging drafts

Two drafts that live here until they are submitted upstream. Neither is a build input of Colligo and neither has been submitted: the bioconda-recipes and nf-core/modules pull requests are separate steps.

## `bioconda/`

`meta.yaml`, `build.sh` and `tiny.fastq` (the recipe test). The recipe builds `v0.1.0` with Go, `-trimpath` and `CGO_ENABLED=0`.

**Why the source is a git tag and not a tarball.** The version comes only from the git tag (`ROADMAP.md`), and `internal/buildinfo` reads it from the VCS stamp that `go build` writes inside a git checkout. A GitHub tarball has no `.git`, and its binary reports `colligo dev (unknown)`; measured on `v0.1.0`, with and without `-buildvcs=false`. So the recipe uses `git_url` with `git_rev: v{{ version }}`. Bioconda discourages that (lint `uses_vcs_url`, which the recipe skips), so this is the decision to confirm before submitting.

`build.sh` fails when `colligo version` is not exactly `colligo v<version> (<commit>)`, so a tarball build or a dirty tree (`+dirty`) stops the build instead of shipping a binary with a wrong version. It excludes the files conda-build leaves in the work directory from `git status` for the same reason.

Guidelines consulted on 2026-10-31: [bioconda guidelines](https://bioconda.github.io/contributor/guidelines.html) (hashes, stable URLs, `git_url`), [linting](https://bioconda.github.io/contributor/linting.html) (`uses_vcs_url`, `should_use_compilers`, `compiler_needs_stdlib_c`, `missing_run_exports`, no `noarch` with a compiler), [building locally](https://bioconda.github.io/contributor/building-locally.html). None of these pages has Go-specific guidance (vendoring, module download, license bundling): the Go parts of the recipe follow the `compiler('go')` form of the lint page and general practice, not a bioconda rule.

Tested here: `build.sh` against a clone of `v0.1.0` (version `colligo v0.1.0 (b23beb1…)`, with conda-build's stray files; a stray file makes it fail) and against a tarball (fails, as designed). Not tested: `conda-build` and `bioconda-utils lint` (not installed, nothing was installed), and `go-licenses` (stubbed).

## `nf-core/colligo/check/`

`main.nf`, `meta.yml`, `environment.yml` and a stub-only `tests/main.nf.test`. It takes `tuple val(meta), path(reads)` and emits the JSON of `colligo check --json` (`schema_version` 2; a future MultiQC module reads it), the exit code of colligo, and `versions.yml`.

- `ext.args` goes to `colligo check`, for example `--source ena:ERR5514981`.
- **Exit 3 (blocking) needs a decision.** Today the task reports and continues: the JSON and `*.colligo.exit_code` are published and the pipeline goes on, like FastQC, so that a MultiQC report can show the failure. `ext.fail_on_blocking = true` in `modules.config` makes the task fail instead. Every other non-zero exit (1 usage, 2 fatal, 4 interrupted, 5 quick) always fails the task. The one line to flip the default is `def fail_on_blocking = task.ext.fail_on_blocking ?: false` in `main.nf`.
- `versions.yml` is a file and also goes to the `versions` topic, because the current spec marks the HEREDOC capture as deprecated in favour of `topic: versions`; with `eval` the file would not be needed.

Spec consulted on 2026-10-31: [nf-core modules specifications](https://nf-co.re/docs/specifications/components/modules/) and the live `modules/nf-core/fastqc` module (`main.nf`, `meta.yml`, `environment.yml`, `tests/main.nf.test`) as the template.

Tested here: the shell logic of `script:` replayed by hand for exit 0, exit 3 with and without `fail_on_blocking`, and a missing file. Not tested: Nextflow, `nf-core modules lint` and `nf-test` (none installed).

## What is missing to submit

1. Open the PR to `bioconda/bioconda-recipes` with the recipe under `recipes/colligo/` (the maintainer's action; the `v0.1.0` tag already exists), after deciding `git_url` versus a tarball. A tarball would need a version that does not come from the VCS stamp, which changes `internal/buildinfo` and contradicts "no version constant in the code".
2. Run `bioconda-utils lint` and a real `conda-build`, and confirm that the bioconda CI may download Go modules during the build (otherwise vendor them) and that `license_file: thirdparty` (a directory from `go-licenses`) is accepted.
3. Add the `identifier` (bio.tools) to `meta.yml` once Colligo has one.
4. nf-core wants test data from `nf-core/test-datasets`, not `tests/data/tiny.fastq`, and a real (non-stub) test with its snapshot; the container and conda lock entries of `meta.yml` are generated after the bioconda package exists.
5. Open the PR in `nf-core/modules` with `nf-core modules create`-style scaffolding, after the bioconda package is published (the `environment.yml` pins `bioconda::colligo=0.1.0`).
6. Decide the exit 3 behaviour above.
