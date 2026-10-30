# Colligo — Conventions

House style. Inviolable during implementation. Short on purpose.

---

## Code

**No comments. None.** Not in Go, not in SQL, not in example TOML. Names communicate intent. If a piece of code needs a comment to be understood, it needs to be rewritten, not commented.

**Errors are values and are handled.** No `_` swallowing an error outside genuinely irrelevant cases. Domain errors are typed (SPEC §10) and inspectable via `errors.As`. Every error carries `Severity()`.

**Path, URL and credential are never `string`.** Use `secret.Name`, `secret.Endpoint`, `secret.Secret`. This is not a style preference: it is the mechanism that prevents leakage. Code that passes a raw `string` to a log field does not compile, and the analyzer of `CONFIDENTIALITY.md` §3.1 rejects the side doors. Typed errors follow the same rule.

**Concurrency via `errgroup` with `SetLimit`.** Never a loose `go func`. An error cancels the siblings via context; all of them are waited for. All concurrent code is tested with `-race`.

**`context.Context` is the first parameter** of every operation that does I/O or can be cancelled. Cancellation saves partial progress before shutting down.

**Functional options** for public construction. Minimal use has sensible defaults; fine control is optional.

**No `init()` with side effects.** Sources are registered explicitly via an option. No mutable global state, no initialization order mattering.

**File content is never in memory.** Streaming to disk, always. `io.CopyBuffer` with a 1MB buffer.

**One read per file.** Everything derived from the content — hashes, tree, structure, counts — comes out of the same read, distributed in parallel to one goroutine per consumer (SPEC §7). `io.MultiWriter` is forbidden on this path, because it serialises the consumers. An extractor does not open a file or seek. Reading twice is a bug.

**The hot path does not allocate.** Parsers and extractors operate on the buffer they receive. An allocation per record is a performance bug and has a test with `testing.AllocsPerRun`.

**A fact is a pure function of the content.** An extractor does not read name, path, mtime or config. If what it computes changes, bump the version.

**The light door does not import `store`.** Every new feature first asks whether it works without a database. If it does, it is born in the core and the heavy door only persists it.

**Adapters are pure.** They do not receive a `Store`, do not receive a disk path, do not open files. An adapter that needs that is solving the wrong problem.

**Stdlib first.** An external dependency only where the stdlib is genuinely insufficient. The conscious ones of v1: `modernc.org/sqlite`, a BLAKE3 package, `golang.org/x/sync/errgroup`, `golang.org/x/time/rate`, a zstd package, an isolated FTP client. Any addition needs a justification.

---

## Database

**Never write to a Scribo table.** `result_inputs` and `input_signals` are read-only for Colligo. `CONTRACT.md` is identical in both repositories; CI compares the copies.

**State transitions never overwrite. They are new rows in `file_events`, always with `run_id`. The database is the canonical state; TSV and manifest are exports.

**A `done` file is never overwritten.** Different content at the same URL creates a new row with `supersedes`. There is no code path that does a destructive UPDATE on `done`.

**All writes go through the writer goroutine.** Workers do not write directly to SQLite.

**PRAGMAs always, in the DSN, and read back.** No connection opens without `foreign_keys=ON`, journal mode resolved by the filesystem (`WAL` local, `DELETE` on a network), `synchronous=NORMAL`, `busy_timeout=5000` and `_txlock=immediate` — and without confirming that they took effect. Opening identical to Scribo's, per `CONTRACT.md` §2. Never `db.Exec("PRAGMA …")` on a `*sql.DB`: it is a pool, and only one connection receives it.

**A migration runs in a transaction**, together with the record in `schema_migrations`. Half an applied migration is a state that nobody knows how to reconstruct.

**`STRICT` tables, timestamps in UTC RFC 3339.**

**The `store` layer decides about restricted data, not the caller.** `SaveRawResponse` consults the classification and writes the body or the hash. There is no parameter to turn it off.

---

## Tests

**Offline and deterministic.** A fake HTTP server with versioned fixtures. No PR test touches the network.

**Explicit fault injection.** Range ignored, truncated body, reset, 429, 412, `ENOSPC`, failing `fsync` — each one has its own test. This is where robustness is proven.

**Every adapter passes `sourcetest`.** Without exception, including private adapters.

**A fixture is recorded, never written by hand.** Two categories: `testdata/synthetic/` versioned, `testdata/real/` never. Details in `TESTING.md`.

**A test that depends on a real fixture fails when it is missing.** It never skips silently.

**A test written together with the code**, not after. Executable examples where they make sense as living documentation.

**The transition table is a variable, and the test reads from it.** The test is the spec, not a copy of it.

---

## Documentation

**GoDoc only where it adds something.** On an exported identifier, and only when it says what the name and the signature do not: an invariant, a usage restriction, a non-obvious why. GoDoc that repeats the name is noise and goes. It is the only form of explanatory text allowed in the code; an unexported identifier takes no comment.

**Language.** Docs, code, GoDoc, test names and messages, error messages, diagnostics and CLI output are in English, like the public README. The private `CLAUDE.md` may stay in Portuguese.

**A decision changes code and doc in the same commit.** SPEC and implementation do not drift apart.

**Where each thing lives:** the why in `DESIGN.md`, the what in `SPEC.md`, defence against silent error in `RELIABILITY.md`, protection of restricted data in `CONFIDENTIALITY.md`, how to test in `TESTING.md`, how to contribute in `CONTRIBUTING.md`, what was postponed in `BACKLOG.md`, what already exists outside here in `RELATED.md`.

---

## Git

**`CLAUDE.md` is private** (`.gitignore`). It does not go into the repository.

**Mandatory `.gitignore`:** `testdata/real/`, `*.local.toml`, `*.db`, `*.db-wal`, `*.db-shm`, `.colligo/`.

**`gitleaks` in pre-commit.** A leaked credential cannot be un-leaked.

**Nothing enters `main` without a pull request.** All work happens on a branch, CI runs on every PR (and on `main` after each merge, not on the push of a branch without a PR), and the merge only happens after review.

**Branch name:** `<type>/<description>`, with the type from the list below and the description in English, lowercase, words separated by hyphens, only `a-z`, `0-9` and `-`, up to 50 characters in total. An issue number, when there is one, opens the description: `fix/12-bgzf-bsize`. The branch says what changes; the work session is recorded in the PR.

**Commit message** (Conventional Commits):

```
<type>(<scope>): <summary>

<body: why, not what>

<trailers>
```

- Types: `feat`, `fix`, `test`, `docs`, `refactor`, `perf`, `ci`, `build`, `chore`, `style`, `revert`. There is no `wip`.
- Optional scope: package or area (`format`, `facts`, `verifier`, `cli`, `corpus`, `verify-cert`).
- Summary in English, imperative, no trailing period, up to 72 characters; lowercase, except an acronym or proper name.
- Body mandatory in `feat` and `fix`: it explains the why. The "what" is in the diff.
- Incompatible change: `!` after the type and a `BREAKING CHANGE:` line in the body.
- Trailers: `Co-Authored-By:` when an agent wrote the commit; `Refs: #N` to link an issue or PR.
- The PR title follows the same format. The merge commit generated by GitHub is the only exception.

Small commits, by slice or sub-slice. `.github/check-names.sh` checks the branch name, PR title and each commit of the PR in the PR's CI; a branch or message outside the pattern turns the PR red. The rule applies from the PR that introduced it onwards; the earlier history is not rewritten by it.

**`main` is protected on GitHub** by the rule versioned in `.github/rulesets/main.json`: no direct push, no `push --force`, no deleting the branch, and merge only by PR with the `test` check (the CI job, published by GitHub Actions) green and up to date with `main`. Merge commits only, to preserve the small commits. Nobody has a bypass, not even the org owner. The free plan only applies the rule to a public repository; it enters on the day the repository becomes public:

```
gh api -X POST repos/colligo-go/colligo/rulesets --input .github/rulesets/main.json
```

Once active, the history of `main` is never rewritten again. Any history cleanup happens before. GitHub does not offer, on the free plan, a rule for branch name or commit message; those continue to be checked by CI.

**Public repository since Slice 0.**
