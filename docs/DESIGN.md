# Colligo — Design e Decisões

O propósito deste documento é registrar **por que** o Colligo é como é. Quem quer saber **como** implementar deve ler `SPEC.md`. Quem quer saber **o que vem depois** deve ler `ROADMAP.md` e `BACKLOG.md`.

---

## 1. Visão

Colligo é uma biblioteca Go para aquisição, validação e persistência de dados genômicos de fontes públicas. O nome vem do latim _colligere_ — "eu coleto, eu reúno, eu agrego".

Nasce da necessidade de um pipeline de download robusto para epigenômica e transcriptômica, mas é agnóstico ao tipo de experimento por design.

**Faz:** busca metadados em fontes públicas, valida o declarado contra o observado, baixa arquivos com verificação de integridade e resume, persiste tudo em SQLite com histórico auditável, exporta para TSV on-demand.

**Não faz:** análise (alinhamento, peak calling, expressão diferencial), gerenciamento de pipeline (Snakemake, Nextflow), visualização.

Modo de uso primário é como biblioteca importada. Um CLI companion existe na v1 com dois comandos mínimos (`fetch` e `verify`); o resto é fase posterior.

---

## 2. Princípios

**O usuário é a fonte de verdade.** Ele declara o tipo de dado antes de começar. O Colligo usa a API para confirmar, não para descobrir.

**Nunca silencioso sobre problemas.** Qualquer discrepância, inferência ou metadado duvidoso gera warning visível e fica registrado. O pipeline pode continuar, mas o usuário sempre sabe o que foi assumido.

**Dado parcial tem valor.** Um arquivo de 50GB baixado até 80% representa horas de transferência. Nunca se apaga progresso parcial. Resume é sempre preferível a recomeçar.

**Auditabilidade total.** Toda transição de estado e toda chamada de API ficam registradas com timestamp. O usuário reconstrói exatamente o que aconteceu.

**Extensibilidade por design.** O core é agnóstico ao tipo de experimento. Novos tipos e fontes entram implementando interfaces, sem tocar no core.

---

## 3. Decisões cravadas e seus porquês

### Driver SQLite: `modernc.org/sqlite` (Go puro, sem CGO)

A meta de multiplataforma com binário simples é incompatível com CGO na prática — cross-compilar `mattn/go-sqlite3` para Linux/macOS × amd64/arm64 dói a cada release e inviabiliza Windows. O `modernc` é mais lento, mas o gargalo do Colligo é sempre rede e disco dos arquivos, nunca o banco de metadados. Pagamos uma moeda que sobra (microssegundos no banco) para comprar uma que falta (sanidade no build).

Consequência: a documentação **não** afirma "tudo stdlib". É "stdlib + um driver SQLite puro-Go".

### PRAGMAs obrigatórios na abertura da conexão

`foreign_keys=ON` porque o SQLite não enforça foreign keys por padrão — sem isso toda a integridade referencial do schema é decorativa. `journal_mode=WAL` para leituras concorrentes com escrita. `synchronous=NORMAL` porque é seguro sob WAL e muito mais rápido que FULL — é o maior ganho de escrita de graça. `busy_timeout=5000` para que contenção residual espere em vez de virar erro.

### Resume reconcilia contra o disco

O offset salvo no banco não é confiável sozinho: o processo pode morrer entre o flush dos bytes e o write do offset, e disco e banco divergem. Na volta, a verdade é o disco. Faz-se `stat` no `.part`, pega-se o menor entre disco e banco, recua-se à fronteira do último chunk BLAKE3 verificado, e reverifica-se dali. Exige-se resposta `206`; um `200` (servidor ignorou o `Range`) é erro, não continuação.

Este é o ponto onde robustez se ganha ou se perde. Tem teste de propriedade: matar o download em offset aleatório N vezes e afirmar que o checksum final sempre bate.

### Integridade interna é BLAKE3; MD5/SHA da fonte é juiz único

BLAKE3 é uma árvore de Merkle por construção: paraleliza um único arquivo em vários núcleos, passa de vários GB/s com aceleração de hardware, e localiza o dano no nível do bloco. MD5 fica preso em ~600 MB/s num núcleo só, sem aceleração. Por isso a integridade contínua (resume, reverificação, reparo) usa BLAKE3 por chunk, e o MD5/SHA da fonte serve só para um cross-check do arquivo inteiro, no fim do primeiro download, calculado de graça no streaming.

### A grade de chunks unifica resume, reparo e verificação

Em vez de "um arquivo com um offset", o download é uma grade de blocos de tamanho fixo, cada um com seu BLAKE3. Resume, reparo de bloco corrompido e scrub periódico são a mesma varredura: para cada bloco, "bate com o esperado?". Os que não batem (nunca baixados, ou corrompidos) são rebaixados via `Range` — e só eles.

A grade de hashes é construída pelo Colligo durante o primeiro download, não recebida da fonte. Os hashes de chunk são mantidos para sempre (são baratos) porque habilitam reverificação eterna. Tamanho de chunk inicial: 8–16MB, equilibrando número de linhas na tabela e granularidade de reparo.

Limitação aceita: corrupção que desloca bytes (não corrompe no lugar) faz todos os blocos seguintes falharem o hash, então o reparo rebaixa do ponto do erro até o fim em vez de um bloco só. Continua correto, só perde a economia nesse caso raro. Não tentamos resync por conteúdo (rolling hash estilo rsync) na v1.

### Verificação como funil

A verificação mais rápida é a que não lê o arquivo. Funil: gate de `stat` (tamanho/mtime/inode iguais ao baseline → pula) → BLAKE3 numa passada para o que sobrar → scrub amostral periódico (verifica fração rotativa por `last_verified_at`, teto de I/O por execução). Modos `--trust-mtime` (rápido) e `--deep` (relê tudo, pega bit rot silencioso).

Concorrência de verificação ≠ de download: em HDD, leituras concorrentes derrubam o throughput; default baixo (1–2), só sobe em flash.

### Writer goroutine único, com erro e flush

Um único goroutine serializa todas as escritas no SQLite, eliminando contenção. Mas não pode ser fire-and-forget: cada evento carrega um canal de resposta, e o worker espera o ack — senão um disco cheio passa despercebido e o estado se perde. No shutdown: cancela context, para de aceitar eventos, drena o canal, commita, faz fsync, sai. É o que torna a promessa de "progresso salvo antes de encerrar" verdadeira.

### `SourceFor` via TOML, prefixo só como fallback

Detecção por prefixo colide (`GSE` em ENA e Entrez, `GCA` em ENA e NCBI) e não carrega config (Entrez precisa de API key/email). A fonte declarada no TOML é o caminho primário; o prefixo é fallback; prefixo ambíguo falha pedindo declaração explícita — coerente com "nunca silencioso".

### FTP isolado, HTTPS prioritário, Aspera fora da v1

ENA serve quase tudo por HTTPS — preferimos isso e herdamos `Range`, retry e checksum do caminho principal. FTP fica num pacote isolado, com resume próprio, só onde não há alternativa (cantos de Ensembl/UCSC). Aspera (`ascp`) é binário externo com licença e configuração de rede fora do nosso controle: explicitamente fora da v1.

### Concorrência idiomática

Orquestração com `errgroup` e `SetLimit`: um erro cancela os irmãos via context e espera-se todos. O exemplo da documentação reflete isso, porque é o primeiro código que um adotante copia.

### Infraestrutura

Go: estável mais recente (habilita iteradores `iter.Seq`, que resolvem iteração sobre estudos grandes sem carregar tudo em memória). Licença: MIT (fricção zero para o público de pesquisa; compatível com migração futura para Apache se necessário). Módulo: `github.com/<org>/colligo`.

---

## 4. Tipos de dado

Sete na v1, todos como perfis plugáveis sobre um core agnóstico, mais o `generic` como fallback sem schema.

| Tag           | Tipo                  | Fontes principais   | Controle          |
| ------------- | --------------------- | ------------------- | ----------------- |
| `chipseq`     | ChIP-seq              | ENCODE, ENA, GEO    | Sim — Input/IgG   |
| `rnaseq`      | RNA-seq               | GEO, ENA, ENCODE    | Não               |
| `atacseq`     | ATAC-seq              | ENCODE, GEO, ENA    | Não               |
| `methylation` | WGBS / MeDIP-seq      | GEO, ENA            | Não               |
| `wgs`         | WGS / WES             | ENA, SRA            | Depende do design |
| `singlecell`  | scRNA-seq (aquisição) | GEO, ENA            | Não               |
| `genome`      | Genoma de referência  | NCBI, Ensembl, UCSC | N/A               |
| `generic`     | Desconhecido          | Qualquer            | Não               |

`singlecell` é **aquisição apenas**: valida metadados de biblioteca (10x Chromium, Drop-seq, Smart-seq2, versão de química, layout). Validação de estrutura de output (matriz esparsa, h5ad, Cellranger) fica explicitamente fora do escopo — é do pipeline downstream. Robustez vem de fazer menos bem feito, não mais razoavelmente.

`role` (signal | control | reference) é mais fundamental que sample_type — toda estrutura experimental tem "o que meço" vs "meu controle".

---

## 5. Sistema de metadados em camadas

**Camada 1 — Core universal:** todo dado tem accession, source, role, layout, replicate, status, confidence, timestamps.

**Camada 2 — Perfis:** cada tipo define roles válidos, lógica de linking signal↔control, campos obrigatórios, validações. Usuário pode registrar perfil customizado.

**Camada 3 — Declaração:** o que o usuário escreve no TOML. O perfil expande a declaração mínima num contrato completo.

**Camada 4 — Confiança:** cada metadado carrega origem — `declared` (explícito da API), `inferred` (heurística, gera warning), `confirmed` (usuário validou). Exportado no TSV como `QC_Status`.

Validação em duas fases: local síncrona (campos, tipos, formato de accession — barata, para antes da rede) e remota assíncrona em fan-out (tipo declarado bate com a API? accessions existem? estrutura confere?).

---

## 6. Relação com o Scribo

Ferramentas separadas e independentes. Colligo adquire dados externos; Scribo persiste resultados de pipeline. Integração por convenção, não por acoplamento: mesmo formato de banco, mesmas convenções de tabela, mesmos padrões de erro. Um pipeline que usa os dois parece coeso sem que o código se conheça.
