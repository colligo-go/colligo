# Packaging drafts

Two drafts that live here until they are submitted upstream. Neither is a build input of Colligo and neither has been submitted: the bioconda-recipes and nf-core/modules pull requests are separate steps.

Both target `v0.2.0`, the first release with `--source` and the `schema_version` 2 report that the recipe test and the nf-core module rely on. The tag exists since 2026-11-01, and the recipe pins its tarball.

## `bioconda/`

`meta.yaml`, `build.sh` and `tiny.fastq` (the recipe test). The recipe builds with `compiler('go-nocgo')`, `-trimpath` and `CGO_ENABLED=0`. The binary is static, so it needs no `stdlib('c')`; `compiler('go')` is not used because it resolved a Go older than the 1.26.3 that `go.mod` asks for, while `go-nocgo` resolves the newest one.

**Source: the release tarball, with its checksum.** bioconda requires a checksum for every source, so the recipe downloads the GitHub tarball of tag `v{{ version }}` and pins its `sha256`. A tarball has no `.git`, so `go build` writes no VCS stamp; `build.sh` therefore injects the version and the commit of the tag with `-ldflags -X` into `internal/buildinfo`, which uses them only when the binary has no stamp (a git checkout and `go install …@v…` keep reading the stamp). The code still holds no version constant: without stamp or injection the binary says `colligo dev (unknown)`.

At each release, fill the two values at the top of `meta.yaml`: `sha256` (`sha256sum` of the tarball) and `commit` (`git rev-list -n 1 v<version>`). For `v0.2.0` they are filled: the commit inside the GitHub tarball (`git get-tar-commit-id`) equals the commit of the tag, and `conda-build` passed against the real GitHub tarball on 2026-11-01, with the version guard of `build.sh` and the three recipe tests. `build.sh` refuses to build when `commit` is not a 40-character commit, and fails when `colligo version` is not exactly `colligo v<version> (<commit>)`, so a recipe with a missing or wrong value never ships a binary with a wrong version.

Tested on 2026-10-31 with conda-build 25.7.0 and the bioconda build configuration of bioconda-utils 4.1.0, against a tarball made by `git archive` (as GitHub makes its release tarballs) of the branch, with its `sha256` and commit filled in:

- `conda-build` passes end to end: Go 1.27.3, the injected version checked by `build.sh`, the bundled licences, and the three recipe tests (`colligo version` reports `v0.2.0`, `colligo check` passes on `tiny.fastq`, the JSON has `schema_version` 2).
- With `commit` left empty, `build.sh` stops with `COLLIGO_COMMIT must be the 40-character commit of tag v0.2.0` and no package is made.
- `bioconda-utils lint`, with the real GitHub address and a filled `sha256`: all checks OK.
- A test command that contains `": "` must be quoted in YAML, or it is read as a mapping and conda-build stops with `TypeError: write() argument must be str, not dict`.

Guidelines consulted on 2026-10-31: [bioconda guidelines](https://bioconda.github.io/contributor/guidelines.html), [linting](https://bioconda.github.io/contributor/linting.html), [building locally](https://bioconda.github.io/contributor/building-locally.html). None of these pages has Go-specific guidance; the Go parts follow the conda-forge pinning (`go_compiler: go-nocgo`) and general practice.

## `nf-core/colligo/check/`

`main.nf`, `meta.yml`, `environment.yml` and `tests/main.nf.test`. It takes `tuple val(meta), path(reads)` and emits the JSON of `colligo check --json` (`schema_version` 2; a future MultiQC module reads it) and `versions.yml`, also published to the `versions` topic.

- `ext.args` goes to `colligo check`, for example `--source ena:ERR5514981`.
- **Any non-zero exit of colligo fails the task**, including 3 (blocking): a pipeline must not go on aligning a shortened read 2. To only report, set `ext.args = '--permissive'`: colligo then exits 0, the task succeeds, and the JSON still carries the failed assertions and the `invalid` status of each file, for MultiQC to show. nf-core does not accept custom `ext` keys, so this uses the flag colligo already has.

Tested on 2026-10-31 with Nextflow 26.10.4 and nf-core/tools 4.2.1:

- Run in Nextflow with a binary built from `main`, on a valid pair and a pair whose read 2 has one read fewer: by default the valid pair succeeds and the other task fails with exit status 3; with `--permissive` both succeed and the JSON of the broken pair shows `pair_count` failed as `blocking` on two `invalid` files.
- `nf-core modules lint` in a sparse clone of nf-core/modules: 42 passed, 3 failed, all because the bioconda package does not exist yet (`bioconda_version`, `docker_tag`, `singularity_tag`). These go away once `colligo=0.2.0` is on bioconda and the container lines are added. An earlier run also flagged the custom `ext.fail_on_blocking` key and the missing `tests/main.nf.test`; both are fixed.
- Not run: `tests/main.nf.test` with nf-test. It uses the sarscov2 FASTQ of nf-core/test-datasets, like the fastqc module, and has no snapshot file yet; the snapshot is generated by the first nf-test run inside nf-core/modules.

Spec consulted on 2026-10-31: [nf-core modules specifications](https://nf-co.re/docs/specifications/components/modules/) and the live `modules/nf-core/fastqc` module as the template.

## MultiQC module

`tools/multiqc/` is a MultiQC plugin that shows the `*.colligo.json` files this module emits (install and use: `tools/multiqc/README.md`). It is not part of either draft and is not submitted anywhere.

## What is missing to submit

1. Open the PR to `bioconda/bioconda-recipes` with the recipe under `recipes/colligo/` (the maintainer's action), and confirm there that the bioconda CI may download Go modules during the build (otherwise vendor them).
2. Once `colligo=0.2.0` is published, add the container lines to `main.nf`, run `nf-test` inside nf-core/modules to create `tests/main.nf.test.snap`, and run `nf-core modules lint` again.
3. Add the `identifier` (bio.tools) to `meta.yml` once Colligo has one.
4. Open the PR in `nf-core/modules` (the maintainer's action).
