# Colligo

Do I have the data I think I have?

Colligo identifies, verifies and watches genomic data from public and private sources.
Downloading with an MD5 is a solved problem; knowing what the file *means* is not.

```
colligo check SRR2133898_1.fastq.gz SRR2133898_2.fastq.gz
colligo check . --certificate
colligo identify genome.fa.gz
```

Exit codes: 0 verified, 1 usage error, 2 fatal, 3 blocking problem pending, 4 interrupted, 5 quick check found nothing in what it read (content not read in full).

- **identify** — which release, assembly and sequence collection this file actually is, from its content
- **verify** — it is whole, and it is what the source and you declared
- **watch** — it still holds, on disk and at the source

Status: early development. `v0.1.0` closes slice 1, the light door (`check` and `validate`); while the major version is 0, any minor release may change the interface. See `CHANGELOG.md` and `docs/ROADMAP.md`.

License: MIT.
