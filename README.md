# Colligo

Do I have the data I think I have?

Colligo identifies, verifies and watches genomic data from public and private sources.
Downloading with an MD5 is a solved problem; knowing what the file *means* is not.

```
colligo check SRR2133898_1.fastq.gz SRR2133898_2.fastq.gz --source ena
```

- **identify** — which release, assembly and sequence collection this file actually is, from its content
- **verify** — it is whole, and it is what the source and you declared
- **watch** — it still holds, on disk and at the source

Status: pre-release, under active development. See `docs/ROADMAP.md`.

License: MIT.
