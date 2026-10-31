# ENA adapter fixtures

Public metadata only. No FASTQ content is stored here.

## Recorded responses

Captured once on 2026-10-31 with curl 8.22.0. For each accession `ACC`:

```
curl -sS "https://www.ebi.ac.uk/ena/portal/api/filereport?accession=ACC&result=read_run&fields=run_accession,library_layout,fastq_ftp,fastq_md5,fastq_bytes,read_count,base_count,status&format=tsv" -o ACC.tsv
```

| File | Accession | Why it is here |
|---|---|---|
| `DRR021374.tsv` | DRR021374 | `read_count` is per file |
| `ERR12477684.tsv` | ERR12477684 | `read_count` is the sum of the files |
| `ERR5060520.tsv` | ERR5060520 | labelled `SINGLE`, two files that form a pair |
| `ERR5514981.tsv` | ERR5514981 | three files, `read_count` is the sum |
| `SRR10663037.tsv` | SRR10663037 | pair plus one unpaired file |
| `ERR0000001-not-found.tsv` | ERR0000001 | accession that does not exist: the API answers 200 with the header only |

Each `.tsv.meta.json` holds the URL, the capture date, the tool and the SHA-256 of the body. A recorded file is never edited: tests mutate it in code.

## Synthetic

`html-500.synthetic.html` is written by hand. It has the shape of the HTML 500 page that the API once returned in place of a TSV.
