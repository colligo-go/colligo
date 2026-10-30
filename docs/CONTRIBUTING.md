# Contributing to Colligo

---

## The contract

To add a source: implement `Source`, record synthetic fixtures, and call `sourcetest.Run`.

If it passes, we accept it without auditing your parsing logic line by line. **What guarantees the user is not our review — it is the suite.** If you find a way to pass the suite and still deliver wrong data, that is a bug in the suite and we want to know.

Adapters are pure: they do not receive a `Store`, do not receive a disk path, do not open files. The worst possible bug in an adapter is returning wrong metadata, which the validator catches. An adapter cannot corrupt the collection, by construction.

---

## Flow of a PR

1. Branch from an up-to-date `main`, named `<type>/<description>`: `feat/ena-source`, `fix/12-bgzf-bsize`.
2. Commits in Conventional Commits: `fix(format): reject a leading UTF-8 BOM`, with a body explaining the why in `feat` and `fix`.
3. Push the branch and `gh pr create`, with a title in the same format. CI runs on the PR, not on the branch push: without an open PR there is no CI. It checks the branch name, title and commits, and runs the whole suite.
4. Merge only with green CI and after review. Nothing enters `main` any other way.

The complete rules are in `docs/CONVENTIONS.md`, Git section.

## Before opening a PR

```
gofmt -l .
go vet ./...
go build -o /tmp/noleak ./internal/noleak/cmd/noleak && go vet -vettool=/tmp/noleak ./...
COLLIGO_PYTHON=<python with blake3> COLLIGO_REQUIRE_PYTHON=1 go test -race -count=2 ./...
go test -run '^$' -fuzz=Fuzz -fuzztime=60s ./source/<your-source>
```

Fuzzing finds cases by chance. When it finds one, in your run, in the PR `fuzz` job or in the weekly job, the input goes into `testdata/fuzz/<Target>/` inside the package that owns the target (for example `format/testdata/fuzz/FuzzValidateFastq/`), committed together with the fix. `go test`, and so the required `test` check, replays it on every run.

`gofmt -l .` must print nothing. The independent verifier needs a Python with the `blake3` package; without `COLLIGO_REQUIRE_PYTHON=1` its test is skipped, and CI does not accept that.

A decision changes code and document in the same commit.

No comments in the code. GoDoc only on an exported identifier, and only when it says what the name and the signature do not. Details in `docs/CONVENTIONS.md`.

---

## Adapter for a private source

An institutional source works. The architecture does not distinguish origin: if you can describe where the files are and what is known about them, it fits in an adapter.

### First: confirm that you need one

A good part of institutional data does not arrive through an API. It arrives as a mounted directory, or an export with a spreadsheet describing the files. In that case **do not write an adapter**: use `colligo adopt --manifest list.csv`. Nothing institution-specific comes to exist in any code, and you get full custody with perpetual reverification.

Write an adapter only if the source really requires a programmatic call.

### The mandatory separation

Three things that must be kept separate:

| What          | Where it lives   | Publishable |
| ------------- | ---------------- | ----------- |
| Code          | repository       | yes         |
| Configuration | local TOML, env  | no          |
| Real fixtures | `testdata/real/` | no          |

**If it is institution-specific, it is configuration, not code.** Applied with discipline, what is left is an adapter that describes a *format*, not an institution — and that is publishable without embarrassment.

Leaks through code, even without a fixture:

- An internal host name written directly in the code
- A field name that reveals what the institution collects (`medical_record`, `birth_date`)
- A project or controlled-study identifier
- A forgotten credential

A leaked credential cannot be un-leaked: Git history is forever. That is why `gitleaks` runs in pre-commit.

### Where the private adapter lives

It does not need to be inside the Colligo repository. It is a separate module that imports Colligo — it can live in an internal repository or just in a folder on your machine.

This works because sources are registered explicitly in `colligo.WithSources(...)`, not by automatic discovery. The core does not need to know that your adapter exists.

### Testing a private adapter

A private adapter has no community review. Nobody but you reads that code. **The conformance suite stops being a convenience and becomes the only reviewer that exists.** Run `sourcetest` just like the public ones, without exception.

A synthetic fixture solves the public test: a fake record with the same shape as the real one, invented values. Understand the limitation — a synthetic fixture tests whether the parser is right, **not** whether reality is still the same. If the server starts returning a new field, your test stays green and you break in practice.

So the work is double:

- **Public:** conformance against a synthetic fixture. Runs for anyone, always.
- **Local:** a test against real fixtures, behind the `restricted` tag, plus the weekly drift job that re-records against the real API. It never leaves your machine or the institution's infrastructure.

The second is where change detection really lives.

### Classification

A restricted source declares `classification = "restricted"` in the configuration. This turns on the whole regime of `CONFIDENTIALITY.md`: response body not persisted, opaque names in the export, database permission enforced, `debug` forbidden.

The declaration is mandatory and has no default. A source without a classification causes a load error.

### A non-technical warning

Publishing the adapter may be restricted by the data use agreement, not only by common sense. Some terms forbid describing the internal structure or the endpoint, even without any data.

Before uploading an adapter that describes an institutional API, ask whoever handles data governance. It is five minutes of conversation against a problem that cannot be undone.

---

## Adding a format validator

Implement `FormatValidator` with a magic prefix and an end-of-file check. Do not do full parsing — the question is "does the file end where it should end?", not "does the content make sense?".

Every validator needs a test with: a valid file, a file truncated at 10 random offsets, a file with 1 byte corrupted in the middle, an empty file, and an HTML error page renamed with the format's extension.

---

## Reporting a bug

Use `colligo redact-report` and paste the output. It is made to be public: it contains versions, config hash, counts of states and error types, and no identifier.

Do not paste a raw log, do not paste a stack trace, do not paste a file path. If the redacted report is not enough to diagnose, that is a bug in the report and we want to know.
