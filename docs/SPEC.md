# Colligo — Especificação Técnica

Documento declarativo. Descreve **o que existe**, sem narrativa. Para o porquê, ver `DESIGN.md`. Para as defesas contra erro silencioso, `RELIABILITY.md`. Para vazamento de dado restrito, `CONFIDENTIALITY.md`. Implemente a partir daqui.

Módulo: `github.com/<org>/colligo`
Go: estável mais recente.
Driver SQLite: `modernc.org/sqlite`.
BLAKE3: `lukechampine.com/blake3` ou `zeebo/blake3`.

---

## 1. Estrutura de pacotes

```
colligo/
├── colligo.go        entry point público: Open(), Config, Export, SourceFor
├── model/            tipos centrais exportados
├── secret/           Secret, Name, Endpoint
├── store/            implementação SQLite de Store; migrations embarcadas
├── source/           uma sub-package por fonte, sem acesso a disco ou banco
│   ├── gencode/
│   ├── ensembl/
│   ├── geo/
│   ├── cbioportal/
│   ├── pdb/
│   └── dfam/
├── transport/        HTTP, FTP, DRS; superfície mínima
├── profile/          perfis plugáveis
├── facts/            leitura única em streaming: raiz BLAKE3, MD5, SHA-256 e fatos de formato
├── downloader/       worker pools, retry, rate limit, resume
├── adopt/            registro de arquivo pré-existente sob custódia
├── verifier/         funil: stat gate, fatos, afirmações, scrub
├── format/           validadores estruturais que também extraem fatos
├── identity/         identificação por conteúdo: seqcol, cabeçalho de GTF, hash contra checksums oficiais
├── compat/           comparação de sistemas de coordenadas por Sequence Collections
├── upstream/         reconsulta de metadados na fonte e comparação com o guardado
├── validator/        validação de metadados em duas fases
├── manifest/         export e atestação de manifesto portátil
├── exporter/         TSV/JSON/CSV com custódia
├── checksum/         árvore BLAKE3 (outboard Bao); MD5/SHA streaming; vetores conhecidos
├── retry/            políticas: exponential, linear, custom
├── sourcetest/       suíte de conformidade de adapters (API pública)
├── storetest/        suíte de conformidade de Store
├── mutants/          gerador de falhas silenciosas para teste e benchmark
├── diagnoses/        catálogo versionado de diagnósticos: observação, causa provável, ação
├── spec/             modelos TLA+ do resume, rename e máquina de estados
├── tools/            verificador independente de certificado (Python)
├── cmd/colligo/      CLI: porta leve e porta pesada (§16)
└── internal/         helpers não exportados
```

**Regra de grafo de import, verificada em teste:** `model`, `store`, `downloader`, `verifier`, `facts`, `format` e `checksum` não importam `source/*`. Registro de fonte é explícito via option; não há `init()` com efeito colateral.

**Regra das duas portas, verificada em teste:** o caminho de `colligo check`, `validate`, `identify` e `compat` não importa `store`. `facts`, `format`, `identity`, `compat` e a parte de metadados de `source/*` não dependem de banco.

---

## 2. PRAGMAs

Abertura conforme `CONTRACT.md` §2, idêntica no Scribo. Aplicar em toda conexão e **ler de volta** para confirmar que surtiram efeito.

```
foreign_keys = ON
journal_mode = <resolvido pelo filesystem>
synchronous  = NORMAL
busy_timeout = 5000
```

Journal mode: `statfs` no diretório do banco; filesystem local → `WAL`; filesystem de rede (`nfs`, `nfs4`, `cifs`, `smb2`, `lustre`, `gpfs`, `fuse.*`, `afs`, `9p`) → `DELETE`, com aviso. WAL exige memória compartilhada entre processos no mesmo host; sobre NFS é corrupção silenciosa. Override explícito registrado em `tool_runs`. Transações de escrita abrem com `_txlock=immediate`.

`*sql.DB` é pool; PRAGMA via `db.Exec` configura uma conexão só. Os PRAGMAs vão no DSN, que o driver aplica a toda conexão nova:

```
file:study.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_txlock=immediate
```

A leitura de volta roda no hook de conexão nova, não uma vez na abertura. Divergência entre valor pedido e valor lido → erro `fatal`. Tabelas usam `STRICT` (SQLite 3.37+). `mmap_size` e `cache_size` ajustáveis.

---

## 3. Schema SQLite

```sql
CREATE TABLE study_meta (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE sources (
    id             TEXT PRIMARY KEY,
    classification TEXT NOT NULL CHECK (classification IN ('public','restricted')),
    declared_at    TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS tool_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    contract_version INTEGER NOT NULL,
    tool             TEXT NOT NULL CHECK (tool IN ('colligo','scribo')),
    tool_version     TEXT NOT NULL,
    git_sha          TEXT NOT NULL,
    go_version       TEXT NOT NULL,
    host             TEXT,
    step             TEXT,
    config_hash      TEXT,
    argv_shape       TEXT NOT NULL,
    started_at       TEXT NOT NULL,
    ended_at         TEXT,
    exit_code        INTEGER
) STRICT;

CREATE TABLE experiments (
    id                  TEXT PRIMARY KEY,
    source              TEXT NOT NULL REFERENCES sources(id),
    type                TEXT NOT NULL,
    condition           TEXT,
    organism            TEXT,
    declared_at         TEXT NOT NULL,
    fetched_at          TEXT,
    expected_file_count INTEGER,
    expected_set_hash   TEXT,
    status              TEXT NOT NULL DEFAULT 'pending'
) STRICT;

CREATE TABLE samples (
    id                 TEXT PRIMARY KEY,
    experiment_id      TEXT NOT NULL REFERENCES experiments(id),
    accession          TEXT NOT NULL,
    role               TEXT NOT NULL,
    replicate          TEXT,
    paired             INTEGER,
    confidence         TEXT NOT NULL,
    source_status      TEXT,
    source_checked_at  TEXT,
    UNIQUE(experiment_id, accession)
) STRICT;

CREATE TABLE files (
    id                 TEXT PRIMARY KEY,
    sample_id          TEXT NOT NULL REFERENCES samples(id),
    origin             TEXT NOT NULL CHECK (origin IN ('fetched','adopted')),
    url                TEXT,
    url_query_stripped INTEGER NOT NULL DEFAULT 0,
    path               TEXT,
    path_part          TEXT,
    format             TEXT,
    format_confidence  TEXT,
    size_expected      INTEGER,
    md5_expected       TEXT,
    sha256_expected    TEXT,
    reads_expected     INTEGER,
    bases_expected     INTEGER,
    blake3_root        TEXT,
    outboard_path      TEXT,
    etag               TEXT,
    last_modified      TEXT,
    pair_id            TEXT,
    supersedes         TEXT REFERENCES files(id),
    status             TEXT NOT NULL DEFAULT 'pending',
    attempts           INTEGER NOT NULL DEFAULT 0,
    last_verified_at   TEXT,
    verified_size      INTEGER,
    verified_mtime     TEXT,
    verified_inode     INTEGER,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    UNIQUE(sample_id, url, supersedes)
) STRICT;

CREATE TABLE file_groups (
    file_id     TEXT NOT NULL REFERENCES files(id),
    idx         INTEGER NOT NULL,
    byte_offset INTEGER NOT NULL,
    size        INTEGER NOT NULL,
    cv          TEXT,
    state       TEXT NOT NULL DEFAULT 'pending',
    PRIMARY KEY (file_id, idx)
) STRICT;

CREATE TABLE facts (
    root              TEXT NOT NULL,
    kind              TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    value             BLOB NOT NULL,
    extracted_at      TEXT NOT NULL,
    run_id            INTEGER REFERENCES tool_runs(id),
    PRIMARY KEY (root, kind, extractor_version)
) STRICT;

CREATE TABLE file_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id      INTEGER NOT NULL REFERENCES tool_runs(id),
    file_id     TEXT NOT NULL REFERENCES files(id),
    from_status TEXT,
    to_status   TEXT NOT NULL,
    reason      TEXT,
    occurred_at TEXT NOT NULL
) STRICT;

CREATE TABLE raw_responses (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        INTEGER NOT NULL REFERENCES tool_runs(id),
    source        TEXT NOT NULL REFERENCES sources(id),
    endpoint      TEXT NOT NULL,
    accession     TEXT NOT NULL,
    body          BLOB,
    body_encoding TEXT NOT NULL CHECK (body_encoding IN ('zstd','none','hash_only')),
    body_hash     TEXT NOT NULL,
    body_size     INTEGER NOT NULL,
    fetched_at    TEXT NOT NULL
) STRICT;

CREATE TABLE discrepancies (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        INTEGER NOT NULL REFERENCES tool_runs(id),
    experiment_id TEXT REFERENCES experiments(id),
    file_id       TEXT REFERENCES files(id),
    field         TEXT NOT NULL,
    declared      TEXT,
    observed      TEXT,
    severity      TEXT NOT NULL CHECK (severity IN ('fatal','blocking','advisory')),
    resolved      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
) STRICT;

CREATE TABLE reference_assets (
    id                 TEXT PRIMARY KEY,
    experiment_id      TEXT NOT NULL REFERENCES experiments(id),
    asset_kind         TEXT NOT NULL CHECK (asset_kind IN ('fasta','transcripts','gtf','gff3','blacklist','proteome')),
    assembly           TEXT NOT NULL,
    release            TEXT,
    release_confidence TEXT NOT NULL,
    chromosome_naming  TEXT,
    seqcol_digest      TEXT,
    coord_digest       TEXT,
    file_id            TEXT NOT NULL REFERENCES files(id)
) STRICT;

CREATE TABLE experiment_references (
    experiment_id TEXT NOT NULL REFERENCES experiments(id),
    asset_id      TEXT NOT NULL REFERENCES reference_assets(id),
    PRIMARY KEY (experiment_id, asset_id)
) STRICT;

CREATE TABLE derived_files (
    id               TEXT PRIMARY KEY,
    path             TEXT NOT NULL,
    format           TEXT,
    blake3_root      TEXT,
    classification   TEXT NOT NULL,
    builder          TEXT NOT NULL,
    builder_version  TEXT NOT NULL,
    params_hash      TEXT NOT NULL,
    built_at         TEXT NOT NULL,
    run_id           INTEGER NOT NULL REFERENCES tool_runs(id)
) STRICT;

CREATE TABLE derived_parents (
    derived_id  TEXT NOT NULL REFERENCES derived_files(id),
    parent_kind TEXT NOT NULL CHECK (parent_kind IN ('file','derived')),
    parent_id   TEXT NOT NULL,
    PRIMARY KEY (derived_id, parent_kind, parent_id)
) STRICT;

CREATE TABLE chipseq_samples (
    sample_id       TEXT PRIMARY KEY REFERENCES samples(id),
    target          TEXT,
    input_sample_id TEXT REFERENCES samples(id),
    antibody        TEXT
) STRICT;

CREATE TABLE rnaseq_samples (
    sample_id    TEXT PRIMARY KEY REFERENCES samples(id),
    treatment    TEXT,
    strandedness TEXT,
    time_point   TEXT
) STRICT;

CREATE TABLE singlecell_samples (
    sample_id    TEXT PRIMARY KEY REFERENCES samples(id),
    library_type TEXT,
    chemistry    TEXT,
    barcode_read TEXT
) STRICT;

CREATE TABLE tabular_samples (
    sample_id   TEXT PRIMARY KEY REFERENCES samples(id),
    kind        TEXT,
    row_count   INTEGER,
    column_hash TEXT
) STRICT;
```

Schemas de `atacseq_samples`, `methylation_samples` e `wgs_samples` a definir ao implementar cada perfil.

`study_meta` guarda: `schema_version`, `study_id`, `classification`, `salt`, `canary_hash`, `created_at`. O `salt` nunca sai daqui.

`argv_shape` guarda a forma do comando com valores removidos (`fetch --config <path> --workers <int>`), nunca os valores.

### Fatos e afirmações

`facts` é indexada pela raiz BLAKE3 do conteúdo, não por arquivo. Um fato é função pura de `(conteúdo, extrator, versão)`: nunca é recalculado enquanto a raiz e a versão do extrator não mudam, e dois arquivos com o mesmo conteúdo compartilham fatos. `value` é JSON canônico.

Tipos de fato da v1:

| `kind`         | Conteúdo                                                        |
| -------------- | --------------------------------------------------------------- |
| `digests`      | MD5, SHA-256, tamanho                                           |
| `structure`    | formato detectado, válido ou não, motivo, offset do problema    |
| `records`      | contagem de registros e de bases, histograma de comprimento     |
| `read_ends`    | primeiro e último ID de read, sem sufixo de par                 |
| `seq_names`    | nomes de sequência amostrados; convenção de cromossomo inferida |
| `seqcol`       | digest de Sequence Collection e `sorted_name_length_pairs`      |
| `gtf_header`   | pragmas do cabeçalho de GTF/GFF3                                |
| `read_header`  | instrumento, run, flowcell e lanes observados nos IDs de read   |
| `barcode`      | comprimento do R1 e taxa de acerto contra whitelists de química |
| `matrix`       | nomes de coluna, contagem de linhas, namespace de ID de gene    |

Afirmações não têm tabela. São comparações calculadas sob demanda entre fatos, colunas `*_expected` e declarações do TOML. Afirmação que falha vira linha em `discrepancies`. O estado de custódia de um arquivo é função do conjunto de afirmações; não existe coluna de custódia atualizada à mão.

`file_groups` guarda os valores de encadeamento (CV) dos nós da árvore BLAKE3 no nível de 8 MiB. O outboard completo, em `outboard_path`, é reconstruível a partir deles. A raiz derivada dos CVs é igual a `blake3_root`; divergência é `fatal`.

### Índices

```sql
CREATE INDEX idx_files_status        ON files(status);
CREATE INDEX idx_files_sample_id     ON files(sample_id);
CREATE INDEX idx_files_verified      ON files(last_verified_at);
CREATE INDEX idx_files_pair          ON files(pair_id);
CREATE INDEX idx_groups_file         ON file_groups(file_id, state);
CREATE INDEX idx_files_root          ON files(blake3_root);
CREATE INDEX idx_samples_exp_id      ON samples(experiment_id);
CREATE INDEX idx_samples_role        ON samples(role, experiment_id);
CREATE INDEX idx_raw_responses_acc   ON raw_responses(accession, source);
CREATE INDEX idx_discrepancies_open  ON discrepancies(resolved, severity);
CREATE INDEX idx_events_file         ON file_events(file_id, occurred_at);
CREATE INDEX idx_events_run          ON file_events(run_id);
CREATE INDEX idx_derived_parents_rev ON derived_parents(parent_id);
CREATE UNIQUE INDEX idx_files_path_unique ON files(path) WHERE path IS NOT NULL;
CREATE UNIQUE INDEX idx_files_part_unique ON files(path_part) WHERE path_part IS NOT NULL;
```

---

## 4. Máquina de estados de arquivo

Cada transição é linha nova em `file_events`, com `run_id`. Nunca UPDATE destrutivo.

| De          | Para        | Gatilho                                       |
| ----------- | ----------- | --------------------------------------------- |
| pending     | queued      | enfileirado                                   |
| queued      | downloading | worker pegou                                  |
| downloading | verifying   | bytes completos                               |
| downloading | failed      | erro de rede/servidor                         |
| verifying   | validating  | checksum global confere                       |
| verifying   | failed      | checksum não confere                          |
| validating  | done        | formato e invariantes de par conferem         |
| validating  | invalid     | formato ou par não confere                    |
| failed      | retrying    | dentro do limite de tentativas                |
| retrying    | downloading | nova tentativa                                |
| failed      | abandoned   | tentativas esgotadas                          |
| done        | stale       | reverificação detectou mudança                |
| stale       | verifying   | revalidação disparada                         |
| invalid     | verifying   | revalidação disparada                         |
| done        | superseded  | fonte publicou conteúdo diferente na mesma URL |
| pending     | adopted     | `adopt` registrou arquivo pré-existente       |
| adopted     | verifying   | verificação de conteúdo disparada             |

Somente `done` conta para completude. A tabela é o código: o mapa de transições válidas é uma variável e o teste de propriedade lê dela.

---

## 5. Contrato de resume

Entrada: um `File` com `path_part` existente e `file_groups`.

1. `stat` no `.part` → tamanho real em disco.
2. Reler do disco os grupos marcados `verified` e recalcular seus CVs. Ponto seguro: fim do maior prefixo contíguo de grupos cujo CV recalculado confere E ≤ tamanho em disco.
3. Truncar o `.part` ao ponto seguro.
4. Requisitar `Range: bytes=<ponto_seguro>-`, com `If-Range` do `etag` ou `last_modified` quando disponíveis.
5. Resposta `200` → `RangeUnsupportedError`, não gravar.
6. Resposta `412` ou ETag divergente → arquivo trocado na origem: transição para `superseded`, `blocking`, não gravar.
7. Resposta `206` → gravar do ponto seguro, calculando o CV de cada grupo, marcando cada grupo `verified` ao fechar.
8. No primeiro grupo gravado da sessão, checar bytes mágicos do formato declarado. Divergência → `fatal`, `.part` preservado.
9. Ao completar: MD5, SHA-256 e os extratores de formato (§7) precisam ter visto o arquivo inteiro. Download sem interrupção: rodaram durante o streaming do passo 7, sem releitura. Download retomado: o estado deles não sobrevive entre sessões, então uma leitura do `.part` inteiro os alimenta. A raiz derivada dos CVs precisa igualar a raiz calculada. Confere com `md5_expected`/`sha256_expected` → `validating`.
10. Em `validating`: afirmações de estrutura, de par se houver `pair_id`, e de contagem se houver `reads_expected`/`bases_expected`. Todas conferem → rename `.part` → final, `done`.

No caminho sem interrupção, o arquivo é lido exatamente uma vez: enquanto chega da rede. Qualquer leitura adicional fora do caso de resume é bug.

Invariante: após qualquer interrupção e retomada, o checksum global final sempre confere. Garantido por teste de propriedade.

`ENOSPC` em qualquer ponto: gravar offset seguro **antes** de propagar o erro.

---

## 6. Contrato de `adopt`

Registra arquivo que chegou por fora — `prefetch`, Globus, disco físico, compartilhamento institucional. É o caminho principal para dado controlado.

Entrada: caminho no disco, fonte, experimento, amostra, formato declarado, checksum esperado (opcional).

1. Verificar que o caminho está sob a raiz do estudo. Fora → erro.
2. `stat`: tamanho e mtime.
3. Uma leitura em streaming produzindo CVs dos grupos, raiz, outboard, MD5, SHA-256 e fatos de formato.
4. Se a raiz já tem fatos em `facts` com a versão corrente dos extratores, a extração de formato é pulada.
5. Afirmações de estrutura e, se houver declaração, de contagem.
6. Registrar `origin='adopted'`, `url = NULL`, `path` relativo à raiz, baseline de verificação.
7. Estado final: `done` se havia checksum esperado e conferiu; `adopted` se não havia esperado; `invalid` se conferiu e falhou.

O arquivo **não é copiado nem movido**. Adoção é registro, não transferência.

Sem `url`, não há resume — não há de onde retomar. Reverificação e scrub funcionam identicamente aos arquivos baixados, que é o propósito inteiro.

`colligo adopt --manifest lista.csv` adota em lote a partir de um CSV com colunas `path,sample,format,md5`.

---

## 7. Funil de verificação

```
verify(file):
    se baseline existe e modo != deep e stat atual == baseline:
        return UNCHANGED
    ler arquivo em buffer de 1MB, recalculando os CVs dos grupos
    se algum CV divergir de file_groups: marcar grupos divergentes; status invalid; return lista
    raiz confere com blake3_root
    para cada kind de fato:
        se facts tem (raiz, kind, versão corrente): reaproveitar
        senão: extrair na mesma leitura
    avaliar afirmações
    atualizar last_verified_at e baseline
    return VERIFIED
```

`UNCHANGED` não é `VERIFIED`. O gate de `stat` decide o que precisa ser lido; não atualiza `last_verified_at` nem leva estado de custódia a `verified`, porque corrupção silenciosa do disco não altera tamanho nem mtime.

Hash igual implica estrutura igual: com a raiz conferindo, fatos de formato só são reextraídos quando a versão do extrator muda.

Modos: `--trust-mtime` (só gate, nunca produz `VERIFIED`), `--deep` (sempre lê conteúdo; reaproveita fatos se a raiz confere, a menos que `--reextract`).

Scrub: seleciona por número de resultados dependentes descendente (contagem em `result_inputs` do Scribo, quando existir), depois por `last_verified_at` ascendente; teto de I/O por execução; cobre o acervo de forma amortizada. Verificar primeiro o arquivo de que mais resultados dependem cobre o risco maior com o mesmo I/O.

Concorrência de verificação: default 1–2, configurável, sobe só em storage flash.

`check`, `validate` e `verify` standalone, stateless, sem banco: aceitam arquivo solto com `--expected`, arquivo de checksums, ou `--source` para comparar contra a fonte. `--cross-check` emite saída compatível com `sha256sum` e `b3sum`.

### Leitura única

Uma leitura do arquivo alimenta todos os consumidores: árvore BLAKE3, MD5, SHA-256, e o extrator do formato declarado, que recebe o fluxo já descomprimido quando o formato é comprimido. Nenhum consumidor lê o arquivo por conta própria.

Distribuição em paralelo, não em série. `io.MultiWriter` chama cada consumidor em sequência na mesma goroutine, e o custo vira a soma de todos; é proibido neste caminho. O leitor preenche buffers de um anel reaproveitado e entrega a mesma fatia, somente leitura, a uma goroutine por consumidor. O buffer volta ao anel quando todos confirmaram. Custo total da leitura: o do consumidor mais lento, não a soma. Ver §12, "Desempenho".

Antes de qualquer verificação: vetores conhecidos de hash e canário do estudo. Ver `RELIABILITY.md` §M9.

---

### Descoberta e diagnóstico

`check .` e `get` classificam cada arquivo pelos bytes mágicos e pela primeira linha, nunca só pela extensão; divergência entre extensão e conteúdo é `advisory`. Pares são formados por sufixo (`_R1`/`_R2`, `_1`/`_2`, `.1`/`.2`) e confirmados pelos fatos `read_ends`; par cujo sufixo casa mas cujos IDs não casam é `blocking`.

Toda afirmação que falha referencia uma entrada do catálogo `diagnoses/`, versionado com o código. Cada entrada tem: identificador, modo de falha de `RELIABILITY.md`, frase de observação com os valores, causa provável, ação sugerida. A saída legível imprime a frase; `--json` inclui o identificador. Afirmação sem entrada no catálogo é bug e falha a suíte.

---

## 8. Writer goroutine

```
workers → canal de eventos (cada evento + chan error) → writer → SQLite
```

Writer único serializa escritas. Worker envia evento e espera ack. Erro de escrita propaga e aborta o pipeline.

Todo evento carrega o `run_id` corrente.

Shutdown: cancelar context → parar de aceitar eventos → drenar canal → commit final → fsync → gravar `ended_at` e `exit_code` em `tool_runs` → sair.

Batch: 100 eventos ou 50ms, o que vier primeiro, com commit forçado no cancelamento. Transições `fatal` são commitadas imediatamente, sem batch.

---

## 9. Interfaces públicas

Adapters são **puros**: não recebem `Store`, não recebem caminho de disco, não abrem arquivo, não escrevem. A parte de metadados de um adapter — `Fetch` e `Resolve` sem download — é o que a porta leve usa em `check --source` e o que `recheck --upstream` reexecuta.

O extrator recebe o conteúdo por `Write`, na leitura única, e devolve fatos em `Finish`. Não abre arquivo, não faz seek, não lê duas vezes. Formato cuja validação exige acesso aleatório é modelado como extrator que acumula só o necessário — o bloco EOF de BGZF são os últimos 28 bytes.

**Fronteira de substituição.** O extrator é a única peça que pode ter implementação fora de Go. Contrato de um extrator externo:

- Executável no PATH com nome `colligo-<formato>`; o Colligo o descobre, nunca o exige.
- Recebe o conteúdo pelo stdin e o formato e a `extractor_version` esperada por argumento.
- Escreve em stdout os fatos em JSON canônico, no mesmo esquema da tabela `facts`, e sai com 0; qualquer outra saída é erro do extrator, não do arquivo.
- Versão diferente da esperada → o Colligo recusa e usa o extrator Go.
- Nunca lê caminho, rede, config nem banco; nunca escreve arquivo.

Todo extrator externo passa pelo teste diferencial contra o extrator Go de mesma versão antes de ser aceito na suíte.

```go
type Source interface {
    ID() string
    Fetch(ctx context.Context, accession string) (Fetched, error)
    Resolve(ctx context.Context, s model.Sample) ([]model.Artifact, error)
}

type Fetched struct {
    Experiment model.Experiment
    Samples    []model.Sample
    Raw        []model.RawResponse
    Issues     []model.ValidationIssue
}

type Artifact struct {
    URL          secret.Endpoint
    Name         secret.Name
    Size         int64
    MD5          string
    SHA256       string
    ReadCount    int64
    BaseCount    int64
    Format       string
    PairID       string
    SourceStatus string
    Headers      map[string]string
}

type Transport interface {
    Get(ctx context.Context, url secret.Endpoint, rng *ByteRange) (*Response, error)
}

type FormatValidator interface {
    Format() string
    Version() string
    MagicPrefix() []byte
    Compressed() bool
    NewExtractor() Extractor
}

type Extractor interface {
    io.Writer
    Finish() ([]model.Fact, error)
}

type Store interface {
    BeginRun(ctx context.Context, meta RunMeta) (int64, error)
    EndRun(ctx context.Context, runID int64, exitCode int) error
    SaveExperiment(ctx context.Context, exp model.Experiment) error
    SaveSample(ctx context.Context, s model.Sample) error
    SaveFile(ctx context.Context, f model.File) error
    SaveGroups(ctx context.Context, fileID string, groups []model.Group) error
    UpdateFileStatus(ctx context.Context, id string, status model.Status, reason string) error
    UpdateGroupState(ctx context.Context, fileID string, idx int, state string) error
    GetPendingFiles(ctx context.Context) ([]model.File, error)
    GetGroups(ctx context.Context, fileID string) ([]model.Group, error)
    SaveFacts(ctx context.Context, root string, facts []model.Fact) error
    Facts(ctx context.Context, root string) ([]model.Fact, error)
    UpdateSourceStatus(ctx context.Context, sampleID string, status string) error
    SaveRawResponse(ctx context.Context, r model.RawResponse) error
    SaveDiscrepancy(ctx context.Context, d model.Discrepancy) error
    FilesForVerification(ctx context.Context, limit int) ([]model.File, error)
    RegisterDerived(ctx context.Context, d model.Derived, parents []model.ParentRef) error
    Completeness(ctx context.Context) (model.Completeness, error)
    Classification(ctx context.Context) (string, error)
}

type Profile interface {
    Type() string
    ValidRoles() []string
    RequiresControl() bool
    LinkStrategy() LinkStrategy
    Validate(s model.Sample) []model.ValidationIssue
}

type Logger interface {
    Debug(msg string, fields ...logf.Field)
    Info(msg string, fields ...logf.Field)
    Warn(msg string, fields ...logf.Field)
    Error(msg string, fields ...logf.Field)
}
```

`SaveRawResponse` decide sozinho gravar corpo ou só hash, consultando a classificação. O chamador não tem opção.

Construção via functional options:

```go
store, err := colligo.Open("study.db",
    colligo.WithLogger(logger),
    colligo.WithSources(gencode.New(), geo.New(geo.WithAPIKeyEnv("COLLIGO_NCBI_API_KEY"))),
)
```

Iteração sobre estudos grandes via `iter.Seq2`.

`sourcetest` e `storetest` são API pública. Adicionar asserção é evento de semver.

---

## 10. Erros tipados

```go
type ChecksumError struct{ ... }
type GroupMismatchError struct{ ... }
type FormatError struct{ ... }
type PairMismatchError struct{ ... }
type IdentityMismatchError struct{ ... }
type SupersededError struct{ ... }
type SourceError struct{ ... }
type TypeMismatchError struct{ ... }
type NotFoundError struct{ ... }
type AuthError struct{ ... }
type RangeUnsupportedError struct{ ... }
type DiskSpaceError struct{ ... }
type IncompleteError struct{ ... }
type CountMismatchError struct{ ... }
type UpstreamChangedError struct{ ... }
type IncompatibleError struct{ ... }
type ClassificationError struct{ ... }
type SelfTestError struct{ ... }
type ClockError struct{ ... }
```

Inspecionáveis via `errors.As`. Todo campo de caminho, URL ou credencial usa os tipos de `secret/` — incluindo `RangeUnsupportedError`, `SourceError` e `ChecksumError`, que hoje carregam `string`. Cada erro carrega `Severity()`.

---

## 11. Estratégia de erro de rede

| Erro                      | Estratégia                                |
| ------------------------- | ----------------------------------------- |
| Timeout / 5xx             | Retry com backoff exponencial + jitter    |
| 429 / 503 com Retry-After | Honrar o header acima do backoff próprio  |
| CV de grupo divergente    | Rebaixar só o grupo                       |
| 200 quando esperava 206   | Permanente (`RangeUnsupportedError`)      |
| 412 ou ETag divergente    | `SupersededError`, blocking               |
| 404                       | Permanente                                |
| 401 / 403                 | Permanente, avisa usuário                 |
| ENOSPC                    | `fatal`, após gravar offset seguro        |

Backoff: `min(2^N, 300s) + jitter`. Rate limiter compartilhado por chave de API.

---

## 12. Concorrência

```
Metadata pool  → 20–50 goroutines, rate limitado por domínio
Download pool  → 4–8 goroutines, limite de conexões por domínio
Verify pool    → 1–2 goroutines
```

Limites por domínio (default, configurável): `ftp.ebi.ac.uk` 4, `ftp.ensembl.org` 2, `ncbi.nlm.nih.gov` 3, `www.cbioportal.org` 2, `files.rcsb.org` 4, `dfam.org` 2.

Estes defaults existem para não queimar o IP da sua instituição. Aumentá-los é sua responsabilidade e fica registrado em `config_hash`.

HTTP: `Accept-Encoding: identity` para arquivos já comprimidos; reuso de conexão via `MaxIdleConnsPerHost`; `io.CopyBuffer` de 1MB. Conteúdo de arquivo nunca em memória.

Concorrência de leitura local ajustada ao armazenamento: vários arquivos ao mesmo tempo em SSD e sistema de arquivos de rede; um por vez em disco rotacional, onde leitura concorrente vira busca aleatória. Default conservador de 2; `--jobs` muda.

### Desempenho

Ordem de custo, do maior para o menor: rede, descompressão, parser, hash, banco. O banco e o driver `modernc` nunca são gargalo.

**Piso físico.** Validar gzip simples e FASTQ exige descomprimir tudo. O piso de `check` completo é a descompressão em Go puro (`klauspost/compress/gzip`) do mesmo arquivo. Meta: `check` completo de um `.fastq.gz` custa no máximo 10% acima desse piso na mesma máquina — isto mede o que é nosso: leitura única, hashes e parser. `zcat arquivo | wc -l` continua medido e reportado; a distância até ele é a do inflate em Go contra o zlib em C, e fechá-la exige cgo ou descompressão especulativa (`BACKLOG.md`), que não entram à custa de confiabilidade. Medido no benchmark de `TESTING.md` §7.1.

**Descompressão.**

- `github.com/klauspost/compress/gzip` no lugar de `compress/gzip`, condicionado a benchmark que mostre ganho na máquina de referência.
- BGZF e BAM: blocos independentes, descomprimidos em paralelo por um pool limitado, com reordenação antes do extrator.
- Gzip simples não tem paralelismo trivial. Descompressão especulativa paralela, como a do rapidgzip, fica no `BACKLOG.md`.

**Parser de FASTQ sem alocação.** Nada de `bufio.Scanner` linha a linha. Contagem de linhas por `bytes.IndexByte` sobre o próprio buffer; soma do comprimento das linhas de sequência; conferência de que qualidade e sequência têm o mesmo comprimento; registro partido entre dois buffers tratado por um resto de no máximo uma linha. Zero alocação por registro, verificado por `testing.AllocsPerRun`.

**Não fazer.** Fatos indexados por raiz tornam a reexecução sobre arquivos inalterados um `stat` por arquivo. A segunda execução é medida e tem meta própria.

**Download com várias conexões.** Um arquivo grande pode ser baixado por várias faixas `Range` em paralelo, cada uma alinhada a grupos de 8 MiB com CV próprio, sem exceder o limite de conexões do domínio. Default: 1 conexão por arquivo; `--connections` aumenta. Faixas terminam fora de ordem; o arquivo só avança para `verifying` quando todos os grupos estão `verified`, e MD5/SHA-256, que são sequenciais, são calculados na leitura final como no caso de resume.

**Verificação sobreposta ao download.** O downloader não grava e depois relê: cada buffer que chega da rede vai, ao mesmo tempo, para o `.part` e para os consumidores da leitura única (§7). Quando o último byte chega, os fatos estão prontos. A única releitura é a do caso de resume (§5, passo 9). Meta: `get` com verificação completa ≤ 105% do tempo de `curl` puro para o mesmo arquivo e servidor.

**Concorrência adaptativa por domínio (AIMD).**

- Começa com 1 conexão por domínio.
- A cada janela de 5 s, se a vazão agregada do domínio cresceu mais de 5% desde a janela anterior, soma 1 conexão (aumento aditivo), até o teto.
- Resposta 429 ou 503, timeout, reset de conexão, ou queda de vazão acima de 20% entre janelas: divide o número de conexões por 2, mínimo 1 (redução multiplicativa). `Retry-After` é honrado acima de tudo.
- O teto é o limite por domínio desta seção, configurável. Os defaults deixam de ser valor fixo e passam a ser teto.
- Cada decisão é registrada em log `debug` com a vazão observada; o resumo por domínio vai para `tool_runs`.

**Modo rápido honesto.** `check --quick` lê só bytes mágicos, o fim do arquivo (bloco EOF de BGZF, trailer de gzip) e uma amostra do início. Leva segundos. O resultado é `quick`, nunca `verified`: o relatório diz que o conteúdo não foi lido por inteiro, e nenhum estado de custódia é atualizado a partir dele.

---

## 13. Manifesto e atestação

`colligo manifest export --out m.json` produz JSON com:

- `schema_version`, `study_id`, `classification`, `redacted`, `created_at`
- `tool`: versão, git SHA
- `files[]`: `id`, `name` ou `opaque_id`, `size`, `format`, `group_size`, `blake3_root`, `outboard` (referência ao arquivo outboard que acompanha o manifesto), `md5`, `sha256`, `reads`, `bases`, `custody_state`, `last_verified_at`
- `derived[]`: `id`, `builder`, `builder_version`, `params_hash`, `parents[]`
- `references[]`: `assembly`, `release`, `release_confidence`, `chromosome_naming`, `seqcol_digest`, `coord_digest`, `asset_kind`

Nunca contém: salt, credencial, URL de estudo restrito, nome real de estudo restrito.

`colligo attest --manifest m.json --root dir` compara o acervo local contra o manifesto e relata, por arquivo: ausente, tamanho divergente, grupos divergentes com índices, ou conforme. A verificação é de fatia contra raiz: quem atesta não precisa confiar em quem produziu o manifesto além da raiz. Saída legível e `--json`. Código de saída 0 se tudo conforme, 3 se não.

Atestação funciona com manifesto redigido, porque hashes não são sensíveis.

### Certificado

`colligo check --certificate` grava `<arquivo>.colligo-cert.json` com: versão do esquema, versão da ferramenta, tamanho, `blake3_root`, `group_size`, MD5, SHA-256, fatos por `kind` com `extractor_version`, e cada afirmação avaliada com declarado, observado e resultado.

O repositório mantém um verificador de referência em `tools/verify_cert.py`: Python com biblioteca padrão mais o pacote `blake3`, ~200 linhas, sem importar nada do Colligo. Ele recalcula tamanho, hashes e os fatos de `records` para FASTQ, confere contra o certificado e confere também as afirmações: `checksum_mismatch`, `size_mismatch`, `count_mismatch` e `structure_invalid` precisam concordar com o que ele recalcula, inclusive o `passed`. A raiz BLAKE3 de arquivo com mais de um grupo é conferida inteira; o outboard não é conferido porque o certificado não o carrega. Divergência entre verificador e certificado é bug de uma das duas implementações e falha a suíte. Códigos de saída do verificador: 0 concorda, 1 discorda, 2 não consegue julgar (uso, entrada ilegível, `schema_version` desconhecido, campo obrigatório ausente ou campo desconhecido) — "não sei" nunca vira "discordo" nem "concordo".

Certificado de estudo restrito segue a tabela de `CONFIDENTIALITY.md` §5.7: nome opaco, sem URL.

### Lockfile

`colligo lock` gera `colligo.lock`: o manifesto reduzido ao que uma análise precisa fixar, em texto, ordenado e estável, feito para ser versionado no repositório da análise — o `go.sum` dos dados.

Uma linha lógica por entrada: nome relativo, `blake3_root`, tamanho, formato, e as identidades confirmadas (`release`, `seqcol_digest`, `coord_digest`, `reads`, `bases`). Sem timestamps, para que o diff entre dois commits mostre só o que mudou de fato.

`colligo check --lock colligo.lock` no início do pipeline confere o acervo local contra o lock e falha fechado. Não exige banco: é porta leve. O diff do lock entre duas versões da análise mostra exatamente qual entrada mudou — um release de 44 para 45, um arquivo substituído.

---

## 14. Configuração

Precedência: defaults < TOML < env < flags.

```toml
[study]
name = "meu_estudo"
db   = "study.db"
mode = "strict"

[sources.gencode]
classification = "public"
rate_limit     = 2

[sources.geo]
classification  = "public"
rate_limit      = 3
credential_env  = "COLLIGO_NCBI_API_KEY"
email           = "pesquisador@instituicao.br"

[sources.dbgap]
classification     = "restricted"
credential_command = "pass show dbgap/token"

[download]
workers   = 8
retry_max = 5
resume    = true

[verify]
workers      = 2
scrub_window = "90d"
scrub_io_cap = "20GB"

[upstream]
recheck_window = "30d"

[cache]
experiment_metadata_ttl = "30d"
file_status_ttl         = "1d"
reference_metadata_ttl  = "90d"
raw_response_ttl        = "90d"

[reference]
assembly = "GRCh38"
release  = "GENCODE 44"

[results]
table  = "rnaseq_qc"
fields = ["Mapped_Reads", "Alignment_Rate", "Assigned_Reads"]

[experiments.GSE116256]
type   = "rnaseq"
source = "geo"
```

`[results]` é opcional: declara a tabela do Scribo e os campos esperados por amostra, que estendem a completude até "processado" (`CONTRACT.md` §7).

`classification` é obrigatório por fonte. Ausência → erro nomeando a fonte.

Credencial em TOML → erro nomeando a chave e a linha. Ver `CONFIDENTIALITY.md` §5.4.

Env: `COLLIGO_DB_PATH`, `COLLIGO_LOG_LEVEL`, `COLLIGO_SYNC_DIRS`, e as variáveis de credencial declaradas por fonte.

Como lib, segredos via options; a lib não opina na origem do segredo.

---

## 14.1 Integração com o Scribo

Conforme `CONTRACT.md`. O Colligo é dono de `samples`, `files` e `facts`, que o Scribo lê. O Colligo lê, e nunca escreve, `result_inputs` e `input_signals`.

**Sinais na custódia.** Na avaliação de afirmações de um arquivo, os sinais de `input_signals` com a mesma raiz entram como afirmações a mais:

| Sinal | Afirmação | Severidade |
| ----- | --------- | ---------- |
| `suspect` | `post_pipeline_consistency` reprovada, com `source` e `detail` | `advisory` |
| `identity_confirmed` | `identity` aprovada | — |
| `identity_mismatch` | `identity` reprovada | `blocking` |

O estado de custódia original nunca é sobrescrito. `status` lista arquivos suspeitos com a fonte do sinal; o certificado inclui a afirmação `identity` quando existe.

**Funil de completude.** `colligo status` mostra declarado → baixado → verificado → processado. "Processado" exige `[results]` no TOML e, para cada amostra, os campos declarados preenchidos na tabela do Scribo e não obsoletos pelo grafo de `CONTRACT.md` §6. `--require-complete` considera o funil inteiro quando `[results]` existe.

**`gc` com proveniência.** Quando implementado (`BACKLOG.md`), classifica cada arquivo por `result_inputs`: sem uso; usado e regenerável a partir de entradas sob custódia; usado e insubstituível.

---

## 15. URIs de fonte

Além de `https` e `ftp`, o campo de URL aceita `drs://` (GA4GH Data Repository Service), resolvido por um resolvedor configurado. Um DRS resolvido produz URL de acesso normal e herda todo o caminho de integridade. Custo baixo, e coloca o Colligo num vocabulário que ferramentas de consórcio reconhecem.

---

## 16. CLI

**Porta leve** — sem banco, sem TOML, sem estado. Não importa `store`.

| Comando    | Função                                                                    |
| ---------- | ------------------------------------------------------------------------- |
| `check`    | fatos + afirmações de arquivos soltos; `--source` compara contra a fonte; `--json` |
| `validate` | só estrutura de formato                                                   |
| `verify`   | standalone com `--expected` ou arquivo de checksums; `--cross-check`      |
| `identify` | identidade por conteúdo: seqcol, cabeçalho de GTF, hash contra checksums oficiais |
| `compat`   | compara sistemas de coordenadas entre FASTA, `.fai`, BAM, VCF, GTF, BED   |
| `check --samplesheet` | confere uma samplesheet de pipeline nf-core pelo conteúdo: arquivos existem, R1 e R2 casam, contagens batem, amostras não se repetem |
| `check --lock` | confere o acervo contra um `colligo.lock`                                 |
| `check --quick` | só bytes mágicos, fim do arquivo e amostra do início; nunca `verified`  |
| `check --cross-extract` | roda extrator Go e externo sobre o mesmo arquivo e compara os fatos; só com extrator externo presente |
| `lock`     | gera `colligo.lock` a partir de arquivos soltos ou de um estudo           |
| `get`      | accession → download com verificação sobreposta → `samplesheet.csv` + `colligo.lock`; sem banco |
| `check .`  | descobre formatos pelo conteúdo e pares pelo nome; zero configuração      |
| `check --certificate` | emite certificado verificável por terceiros (§13)             |

**Porta pesada** — estudo com banco.

| Comando         | Função                                                        |
| --------------- | ------------------------------------------------------------- |
| `fetch`         | fluxo principal: metadados, download, verificação             |
| `verify`        | com banco: funil da §7 e scrub                                |
| `adopt`         | registra arquivo pré-existente sob custódia                   |
| `status`        | estado do estudo; `--require-complete` sai 3 se incompleto    |
| `recheck`       | `--upstream`: reconsulta a fonte e compara com o guardado     |
| `manifest`      | exporta manifesto portátil                                    |
| `attest`        | compara acervo contra manifesto                               |
| `doctor`        | diagnóstico de ambiente                                       |
| `redact-report` | relatório de bug seguro para publicar                         |
| `gc`            | limpeza de `.part` órfão e `raw_responses` expirado; `--dry-run` obrigatório antes de `--force` |
| `export`        | TSV/JSON/CSV com custódia                                     |

**O CLI não contém lógica.** Lê flags, resolve config, chama a biblioteca, traduz erro em código de saída. Comportamento que existe só no CLI é bug.

---

## 17. Requisitos transversais

- Paths armazenados relativos à raiz do estudo, nunca absolutos.
- Sanitizar nome vindo da fonte: rejeitar travessia, NUL, separadores, nome > 255 bytes, e normalizar unicode antes de comparar.
- `.part` e final no mesmo filesystem (rename atômico), verificado por `doctor`.
- Pré-checar espaço com margem de 5%; reverificar a cada 1 GiB.
- Validação Fase 1 (local, zero I/O) antes de qualquer chamada de rede.
- Todo timestamp em UTC, formato RFC 3339.
- `context.Context` é o primeiro parâmetro de toda operação de I/O.
- `-race` em todo teste de concorrência.
- Nenhum acesso de rede a host não configurado. Ver `CONFIDENTIALITY.md` §5.11.
- Cada arquivo é lido uma vez por operação; toda derivação de conteúdo passa pela leitura única da §7.
- Plataforma de desenvolvimento e teste: Linux ou WSL. Windows nativo suporta só a porta leve até existir CI Windows.

---

## 18. Decisões antes abertas, agora resolvidas

| Questão                                   | Decisão                                                                 |
| ----------------------------------------- | ----------------------------------------------------------------------- |
| Retenção de `raw_responses`               | zstd, TTL 90d, `--keep-raw` desliga o TTL; restrito grava só hash        |
| `.part` quando tentativas esgotam         | nunca apagado automaticamente; `gc --dry-run` antes de `--force`         |
| `experiments` ↔ referência                | tabela `experiment_references`, não FK; um experimento admite dois builds |
| Geração de `files.id`                     | `fetched`: `hex(BLAKE3(sample_id \|\| url \|\| supersedes))[:32]`. `adopted`: `hex(BLAKE3(sample_id \|\| path))[:32]`. Re-fetch e re-adoção idempotentes por construção |
| Estrutura de integridade                  | árvore BLAKE3 com outboard Bao; CVs no nível de 8 MiB em `file_groups`; grade de hashes independentes descartada |
| CVs e outboard após verificação final     | todos, para sempre; habilitam reverificação e atestação por fatia        |
| Tamanho de grupo                          | 8 MiB fixo, potência de 2 de chunks de 1 KiB; não configurável na v1     |
| Revalidação estrutural                    | só quando a versão do extrator muda; hash igual implica estrutura igual  |
| Gate de `stat`                            | decide o que ler; nunca produz `verified`                                |
| Batch do writer                           | 100 eventos ou 50ms; `fatal` sem batch                                   |
| Prefetch especulativo                     | fora da v1                                                               |

### Questões abertas

| Questão | Como resolver |
| ------- | ------------- |
| `lukechampine.com/blake3` expõe o CV de uma subárvore em offset arbitrário durante o streaming? O pacote `bao` expõe `Encode` com grupo configurável e `VerifyChunk`. | Verificar na Fatia 1. Se não expuser, manter BLAKE3 por grupo como hash independente só para resume e raiz global à parte, e registrar a troca aqui. |
| Fonte de checksums oficiais do Ensembl para `identify` | Levantar formato do arquivo de checksum por release na Fatia 2 antes de implementar. |

---

## 19. Testes

Ver `TESTING.md`. Resumo dos requisitos que a SPEC impõe:

- Servidor HTTP fake por fonte, fixtures versionadas, offline e determinístico.
- Injeção de falha: Range ignorado, Content-Length errado, corpo truncado, reset, 429 com Retry-After, 412, ENOSPC, fsync falhando.
- Teste de propriedade do resume: matar em offset aleatório N vezes, checksum final sempre confere.
- Teste de propriedade da árvore: grupos pavimentam o arquivo sem lacuna nem sobreposição, e a raiz derivada dos CVs é igual ao BLAKE3 do arquivo inteiro.
- Teste de leitura única: contador de bytes lidos igual ao tamanho do arquivo em `check` e em download sem interrupção.
- Suíte de mutantes (`TESTING.md` §7): toda mutação de cada classe declarada como detectável é detectada.
- Teste de propriedade da máquina de estados: só transições da §4 persistem.
- Fuzzing dos parsers de cada fonte.
- Suíte de conformidade `sourcetest` obrigatória para todo adapter.
- Sentinela de vazamento em todo artefato produzido.
