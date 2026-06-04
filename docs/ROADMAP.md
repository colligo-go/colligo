# Colligo — Roadmap

> **Fatia atual: 1 — Fatia vertical (ENCODE).** Nada fora dela deve ser construído ainda.

A regra deste documento: construir uma fatia fina de ponta a ponta antes de generalizar em largura. A fatia vertical valida os pressupostos mais perigosos (correção do resume, writer único do SQLite, grade de chunks) com código real, antes de qualquer generalização.

---

## Fatia 1 — Vertical (ENCODE, um tipo, um caminho feliz + resume)

O coração de tudo. Quando isto funciona, quase todo o resto é repetição.

Escopo:

- `model/` mínimo: Experiment, Sample, File, Chunk, Status, Confidence.
- `store/` SQLite com os PRAGMAs, migrations, e os métodos que esta fatia usa.
- Writer goroutine com erro e flush.
- `source/encode/` apenas `Fetch` de um experimento real (fixture commitada).
- `downloader/` baixar um arquivo: grade de chunks, BLAKE3 por chunk, streaming para `.part`.
- `verifier/` validação do arquivo inteiro contra MD5 da fonte no fim.
- Resume com reconciliação contra o disco.

Critério de pronto:

- Baixar um arquivo do ENCODE de ponta a ponta, verificar, renomear para final.
- Matar o processo no meio e retomar, com checksum final correto.
- Teste de propriedade do resume passando (matar em offset aleatório N vezes).
- `go test -race` limpo.

Fora desta fatia: outras fontes, outros tipos, validação de discrepância, CLI, export, scrub.

---

## Fatia 2 — Verificação como funil

Sobre a grade de chunks que já existe da Fatia 1.

Escopo: gate de `stat` com baseline, BLAKE3 numa passada, modos `--trust-mtime`/`--deep`, scrub amostral por `last_verified_at`.

Critério: detectar arquivo corrompido apontando o chunk; reverificar estudo grande em segundos no caso comum (gate); scrub cobrir acervo de forma amortizada.

---

## Fatia 3 — CLI mínimo

Escopo: `cmd/colligo` com `fetch` (fluxo principal) e `verify` (arquivo solto, `--expected` ou arquivo de checksums, stateless).

Critério: verificar um FASTQ baixado por fora, sem o banco, pela linha de comando.

---

## Fatia 4 — Validação em duas fases + warnings

Escopo: Fase 1 local (campos, tipos, formato de accession), Fase 2 remota em fan-out, tabela `discrepancies`, warnings chamativos, níveis de confiança.

Critério: declarar tipo errado e ver o warning de type mismatch; inferência gerar `inferred` + warning sem parar o pipeline.

---

## Fatia 5 — Largura: demais fontes

Escopo: `ena/` (HTTPS prioritário), `entrez/` (GEO/RefSeq/SRA), depois `ensembl/`. FTP isolado em `ftp/` só onde necessário. `SourceFor` via TOML com prefixo de fallback.

Cada fonte é uma repetição do padrão validado nas fatias anteriores.

---

## Fatia 6 — Largura: demais tipos

Escopo: perfis e schemas de `atacseq`, `methylation`, `wgs`, `singlecell` (aquisição), `genome`, `generic`. Definir as colunas específicas de cada tabela `*_samples` ao implementar — não antes.

---

## Fatia 7 — Export e polimento

Escopo: `exporter/` TSV/JSON/CSV on-demand com `QC_Status`. Iteração via `iter.Seq2`. GoDoc, exemplos executáveis, README com estudo real.

---

## Tipos de dado por versão

```
v1 — chipseq, rnaseq, atacseq, methylation, wgs, singlecell, genome, generic
v2 — próximos 5 tipos (definir conforme uso real da v1, não no papel agora)
```

Os candidatos de v2 vivem em `BACKLOG.md`. A v1 ensina quais tipos aparecem nos estudos reais — esses são melhores candidatos que os que parecem importantes hoje.

---

## Marcos de versão

```
v0.x.x — interface em desenvolvimento, pode mudar
v1.0.0 — interface pública congelada, semver estrito
```

Permanece em v0 até a interface estar estável e testada em uso real.
