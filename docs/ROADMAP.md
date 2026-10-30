# Colligo — Roadmap

> **Fatia atual: 0 — Fundação mínima.** Nada fora dela deve ser construído ainda.

A regra deste documento: construir uma fatia fina de ponta a ponta antes de generalizar em largura.

Duas regras aprendidas no caminho:

- **O que é estrutural vem antes do que é funcional — mas só o que não dá para encaixar depois.** Tipos que não vazam e verificação da própria ferramenta entram primeiro. Diagnóstico, relatório e regime completo de dado restrito se adicionam depois sem dor, e ficam para quando houver fonte restrita.
- **A porta leve vem antes da pesada.** A primeira entrega útil não tem banco. É ela que gera usuários e é ela que exercita o núcleo — extratores, validadores, adapters — antes de existir persistência. Ver `DESIGN.md` §1, "Duas portas".

---

## Fatia 0 — Fundação mínima

Pequena em linhas, impossível de adicionar depois.

Escopo:

- `secret/`: `Secret`, `Name`, `Endpoint`, com redação por construção.
- `logf`: campos tipados; sem campo `string` genérico para caminho, URL ou credencial.
- `checksum/`: vetores conhecidos de BLAKE3, MD5 e SHA-256 rodados na inicialização.
- Analisador `go/analysis` no CI que proíbe `string` crua onde o domínio exige `secret.*`.

Critério de pronto:

- `%v`, `%s`, `%+v`, `%#v` e `json.Marshal` de todo tipo de `secret/` não contêm a sentinela.
- Vetores conhecidos falham alto quando sabotados de propósito.
- O analisador reprova um fixture de código que passa `string` para `logf.Name`.

Movido para depois: `doctor`, `redact-report`, canário, detecção de pasta sincronizada, permissões de banco, dialer instrumentado, sentinela global. Entram na Fatia 6, junto com a primeira fonte restrita, ou na Fatia 7.

---

## Fatia 1 — Porta leve: `colligo check`

A menor unidade útil. Sem banco, sem TOML, sem estado.

Escopo:

- `facts/`: leitura única em streaming que produz raiz BLAKE3 (outboard Bao, grupo de 8 MiB), MD5, SHA-256 e os fatos de formato.
- `format/`: validadores de gzip, bgzf, fastq, fasta e gtf, que também extraem fatos — contagem de registros e bases, primeiro e último ID, nomes de sequência.
- Invariante de par sobre os fatos: mesma contagem, IDs compatíveis.
- `cmd/colligo check` e `validate`: arquivos soltos, `--expected`, `--json`, códigos de saída de `RELIABILITY.md` §1.
- `--cross-check` com saída compatível com `b3sum` e `sha256sum`.
- Leitura única distribuída em paralelo, sem `io.MultiWriter`; parser de FASTQ sem alocação; `check --quick`.
- Benchmark de desempenho contra o piso do gunzip em Go puro, com `zcat | wc -l` reportado (`TESTING.md` §7.1).
- `check .` com descoberta de formato e pares; catálogo `diagnoses/` com uma entrada por afirmação.
- `check --certificate` e o verificador independente `tools/verify_cert.py`.

Critério de pronto:

- Truncar um `.fastq.gz` em 10 offsets: todos detectados.
- Página HTML renomeada para `.gtf.gz`: rejeitada pelos bytes mágicos.
- R2 com um read a menos que R1: `blocking`, código 3.
- Cada arquivo é lido exatamente uma vez, verificado por contador de bytes lidos no teste.
- Hashes conferem com `b3sum` e `sha256sum` quando presentes.
- `check` completo de um `.fastq.gz` a no máximo 10% acima do gunzip em Go puro na máquina de referência.
- Parser de FASTQ com zero alocação por registro.
- `colligo check .` numa pasta com pares R1/R2 e um arquivo truncado roda sem configuração e imprime o diagnóstico em frase.
- O verificador Python concorda com o certificado em todo o corpus sintético.

---

## Fatia 2 — Declarado contra a fonte

A porta leve passa a perguntar à fonte.

Escopo:

- `source/ena/`: apenas a parte de metadados — `fastq_md5`, `fastq_bytes`, `read_count`, `base_count`, status do registro. Puro, com fixture sintética.
- `transport/` HTTP mínimo para metadados, com rate limit por domínio.
- `colligo check --source ena`: compara fatos contra o declarado. M15 de `RELIABILITY.md`.
- `colligo identify`: digest de Sequence Collection para FASTA; release do GTF do GENCODE pelo cabeçalho; hash contra checksums oficiais onde houver.
- `colligo check --samplesheet`: samplesheet de pipeline nf-core conferida pelo conteúdo.
- Módulo MultiQC que lê o `--json` do `check` e mostra o resultado no relatório que o usuário já abre.

Critério de pronto:

- O caso QC Fail reproduzido em fixture — FASTQ válido com read 2 encurtado — sai com código 3 porque `base_count` não bate.
- `identify` reconhece um GTF do GENCODE pelo cabeçalho e um FASTA pelo digest seqcol.
- Uma samplesheet com R2 trocado entre duas amostras é reprovada.
- O resultado do `check` aparece num relatório MultiQC.
- Distribuição: receita bioconda rascunhada e módulo nf-core esboçado.

---

## Fatia 3 — Porta pesada: fetch, store e resume

O núcleo de aquisição, agora com persistência.

Escopo:

- Antes do código: modelo TLA+ do resume, do rename e da máquina de estados, verificado por model checking (`RELIABILITY.md` M20).
- `colligo get`: accession → download → `samplesheet.csv` + `colligo.lock`, sem banco, com estado de resume ao lado dos arquivos.
- Verificação sobreposta ao download; concorrência adaptativa por domínio (SPEC §12).
- Abertura do banco conforme `CONTRACT.md` §2, idêntica à do Scribo: journal mode por filesystem, `_txlock=immediate`, `tool_runs` com o DDL do contrato.
- `model/` e `store/` SQLite conforme SPEC, com PRAGMAs no DSN lidos de volta e tabela `facts`.
- Writer goroutine com erro e flush; `tool_runs` com `run_id` em todo evento; detecção de execução interrompida.
- `source/gencode/` com `Fetch` e `Resolve`, puros.
- `transport/` com `Range`, `If-Range`, retry e rate limit.
- `downloader/`: streaming para `.part` alimentando a mesma extração de fatos da Fatia 1; várias conexões por arquivo alinhadas a grupos, desligado por default.
- Resume com reconciliação contra o disco usando o outboard Bao.
- `cmd/colligo` com `fetch`, `verify` e `status`.

Critério de pronto:

- Baixar FASTA e GTF do GENCODE de ponta a ponta, validar, renomear.
- `colligo get` de um estudo pequeno do ENA entrega samplesheet validada e lock.
- `get` com verificação completa a no máximo 5% acima de `curl` puro.
- Servidor fake com capacidade limitada e 429: a concorrência adaptativa converge sem estourar o teto e sem erro permanente.
- Crash em qualquer ponto: todas as invariantes do modelo valem após cada retomada.
- Matar o processo no meio e retomar, com checksum final correto.
- Teste de propriedade do resume passando.
- `ENOSPC` injetado: offset seguro gravado antes do erro.
- `go test -race -count=2` limpo.

---

## Fatia 4 — `adopt`, scrub, manifesto e atestação

O que destrava os terabytes que chegam por fora.

Escopo: `adopt/` individual e em lote por CSV; gate de `stat` que decide o que ler, sem declarar nada verificado; scrub amortizado por `last_verified_at`; `manifest/` com raiz e outboard; `attest`; `colligo lock` e `check --lock`; duplicata entre accessions por raiz (M19).

Critério: adotar um arquivo vindo de `prefetch` e reverificá-lo depois apontando a fatia corrompida; exportar manifesto, corromper um byte, e a atestação apontar o índice exato; trocar o release de uma referência e o diff do `colligo.lock` mostrar só aquela linha; segunda execução sobre 10 mil arquivos inalterados dentro da meta de `TESTING.md` §10.

---

## Fatia 5 — Vigilância da fonte, GEO, validação em duas fases

Escopo:

- `colligo recheck --upstream`: reexecuta a parte de metadados dos adapters e compara com o guardado. M16 de `RELIABILITY.md`.
- `source/geo/` via E-utilities; caminho de arquivo pequeno; perfis `rnaseq`, `tabular` e `singlecell`.
- Integração com o Scribo (SPEC §14.1): sinais de `input_signals` na custódia, funil de completude com `[results]`, scrub priorizado por dependentes.
- Química de single-cell declarada × observada (M17) e coerência de matriz suplementar contra os GSMs (M18).
- Validação Fase 1 local e Fase 2 remota; `discrepancies` com severidade; `expected_set_hash`; `status --require-complete`.

Critério: um fixture em que o run passa de público a suprimido gera `discrepancy` `blocking`; um dado 10x v2 declarado v3 é `blocking`; uma matriz com duas colunas trocadas de nome é `blocking`; declarar tipo errado para o pipeline em modo estrito; remover um arquivo do fixture e o estudo deixar de ser completo.

---

## Fatia 6 — Referência, `compat` e primeira fonte restrita

Escopo:

- `reference_assets` com assembly, release, `chromosome_naming` e digest seqcol.
- `compat/`: comparação por `sorted_name_length_pairs` entre FASTA, `.fai`, BAM, VCF; inclusão de nomes para GTF e BED.
- Primeiro adapter de fonte restrita, com o regime completo de `CONFIDENTIALITY.md`: permissões, pasta sincronizada, sentinela global, dialer instrumentado, canário.

Critério: FASTA GENCODE contra VCF com contigs do Ensembl devolve incompatível com o diff de nomes; declarar release errado no TOML e receber `fatal`; toda invariante de `CONFIDENTIALITY.md` §6 com teste passando.

---

## Fatia 7 — Largura, derivados mínimos, benchmark e polimento

Escopo: `source/cbioportal/`, `source/pdb/`, `source/dfam/`, `source/ensembl/`; perfil `structure`; FTP isolado; `drs://`; `derived_files` mínimo; `exporter/` com custódia; descompressão paralela de BGZF; `doctor` e `redact-report`; benchmark de falhas silenciosas completo (`TESTING.md` §7); GoDoc; exemplos executáveis; README com estudo real.

---

## Tipos de dado por versão

```
v1 — rnaseq, singlecell, tabular, reference, structure, chipseq, generic
v2 — definir conforme uso real da v1, não no papel agora
```

`atacseq`, `methylation` e `wgs` vivem no `BACKLOG.md`. A v1 ensina quais tipos aparecem nos estudos reais — esses são melhores candidatos que os que parecem importantes hoje.

---

## Marcos de versão

```
v0.x.x — interface em desenvolvimento, pode mudar
v1.0.0 — interface pública congelada, semver estrito
```

Permanece em v0 até a interface estar estável e testada em uso real. A porta leve pode ganhar releases próprias de v0 desde a Fatia 1.

**A versão vem só da tag do git.** Não existe constante de versão no código. Cada fatia fechada sobe a minor: Fatia 1 → `v0.1.0`, Fatia 2 → `v0.2.0`. Correção publicada sobre uma release sobe o patch: `v0.1.1`. Entre tags, o binário se identifica pela pseudo-versão do Go, com data e commit (`v0.0.0-20261029173537-1d27b8e84ac6` antes da primeira tag), e com `+dirty` quando foi compilado com mudança não comitada. `colligo version` e todo certificado registram versão e commit, de modo que qualquer resultado aponta para o código exato que o produziu.

Tag só a partir de `main` com o CI verde, anotada, com as notas da versão vindas do `CHANGELOG.md`.

O repositório é público desde a Fatia 0. Não por ideologia: desenvolver fechado torna o projeto inelegível para os veículos que pedem histórico público de desenvolvimento, e cada mês fechado é um mês que não conta.
