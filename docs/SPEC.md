# Colligo — Especificação Técnica

Documento declarativo. Descreve **o que existe**, sem narrativa. Para o porquê, ver `DESIGN.md`. Implemente a partir daqui.

Módulo: `github.com/<org>/colligo`
Go: estável mais recente.
Driver SQLite: `modernc.org/sqlite`.
BLAKE3: `lukechampine.com/blake3` ou `zeebo/blake3`.

---

## 1. Estrutura de pacotes

```
colligo/
├── colligo.go        entry point público: Open(), Config, Export, SourceFor
├── model/            tipos centrais exportados: Experiment, Sample, File, Status, Confidence
├── store/            implementação SQLite de Store; migrations embarcadas
├── source/           uma sub-package por fonte
│   ├── encode/
│   ├── ena/
│   ├── entrez/       GEO + GenBank + SRA
│   └── ensembl/
├── profile/          perfis plugáveis; interface Profile + um pacote por tipo
├── downloader/       worker pools, retry, rate limit, resume
├── verifier/         funil de verificação: stat gate, BLAKE3, scrub
├── validator/        validação de metadados em duas fases
├── exporter/         TSV/JSON/CSV on-demand
├── checksum/         BLAKE3 por chunk; MD5/SHA streaming
├── ftp/              cliente FTP isolado, resume próprio
├── retry/            políticas: exponential, linear, custom
├── cmd/colligo/      CLI: fetch, verify
└── internal/         helpers não exportados
```

---

## 2. PRAGMAs (aplicar em toda conexão, na abertura)

```
foreign_keys = ON
journal_mode = WAL
synchronous  = NORMAL
busy_timeout = 5000
```

Avaliar `STRICT` nas tabelas (SQLite 3.37+) para enforcement de tipo. `mmap_size` e `cache_size` ajustáveis para leitura de estudos grandes.

---

## 3. Schema SQLite

```sql
CREATE TABLE experiments (
    id          TEXT PRIMARY KEY,
    source      TEXT NOT NULL,
    type        TEXT NOT NULL,
    condition   TEXT,
    organism    TEXT,
    declared_at DATETIME NOT NULL,
    fetched_at  DATETIME,
    status      TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE samples (
    id            TEXT PRIMARY KEY,
    experiment_id TEXT NOT NULL REFERENCES experiments(id),
    accession     TEXT NOT NULL,
    role          TEXT NOT NULL,
    replicate     TEXT,
    paired        BOOLEAN,
    confidence    TEXT NOT NULL,
    UNIQUE(experiment_id, accession)
);

CREATE TABLE files (
    id            TEXT PRIMARY KEY,
    sample_id     TEXT NOT NULL REFERENCES samples(id),
    url           TEXT NOT NULL,
    path          TEXT,
    path_part     TEXT,
    size_expected INTEGER,
    size_actual   INTEGER,
    md5_expected  TEXT,
    md5_actual    TEXT,
    sha256        TEXT,
    pair_id       TEXT,
    status        TEXT NOT NULL DEFAULT 'pending',
    attempts      INTEGER DEFAULT 0,
    last_verified_at DATETIME,
    verified_size INTEGER,
    verified_mtime DATETIME,
    verified_inode INTEGER,
    created_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL
);

CREATE TABLE file_chunks (
    file_id       TEXT NOT NULL REFERENCES files(id),
    idx           INTEGER NOT NULL,
    offset        INTEGER NOT NULL,
    size          INTEGER NOT NULL,
    blake3        TEXT,
    state         TEXT NOT NULL DEFAULT 'pending',
    PRIMARY KEY (file_id, idx)
);

CREATE TABLE file_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    file_id     TEXT NOT NULL REFERENCES files(id),
    from_status TEXT,
    to_status   TEXT NOT NULL,
    reason      TEXT,
    occurred_at DATETIME NOT NULL
);

CREATE TABLE raw_responses (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source     TEXT NOT NULL,
    endpoint   TEXT NOT NULL,
    accession  TEXT NOT NULL,
    body       TEXT NOT NULL,
    fetched_at DATETIME NOT NULL
);

CREATE TABLE discrepancies (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    experiment_id TEXT NOT NULL REFERENCES experiments(id),
    field         TEXT NOT NULL,
    declared      TEXT,
    observed      TEXT,
    severity      TEXT NOT NULL,
    resolved      BOOLEAN DEFAULT FALSE,
    created_at    DATETIME NOT NULL
);

CREATE TABLE chipseq_samples (
    sample_id       TEXT PRIMARY KEY REFERENCES samples(id),
    target          TEXT,
    input_sample_id TEXT REFERENCES samples(id),
    antibody        TEXT
);

CREATE TABLE rnaseq_samples (
    sample_id    TEXT PRIMARY KEY REFERENCES samples(id),
    treatment    TEXT,
    strandedness TEXT,
    time_point   TEXT
);

CREATE TABLE singlecell_samples (
    sample_id    TEXT PRIMARY KEY REFERENCES samples(id),
    library_type TEXT,
    chemistry    TEXT,
    barcode_read TEXT
);

CREATE TABLE genome_assets (
    experiment_id TEXT PRIMARY KEY REFERENCES experiments(id),
    assembly      TEXT NOT NULL,
    release       TEXT,
    has_fasta     BOOLEAN DEFAULT FALSE,
    has_gtf       BOOLEAN DEFAULT FALSE,
    has_blacklist BOOLEAN DEFAULT FALSE
);
```

Schemas de `atacseq_samples`, `methylation_samples` e `wgs_samples` a definir ao implementar cada perfil (ver ROADMAP).

### Índices

```sql
CREATE INDEX idx_files_status      ON files(status);
CREATE INDEX idx_files_sample_id   ON files(sample_id);
CREATE INDEX idx_files_verified    ON files(last_verified_at);
CREATE INDEX idx_chunks_file       ON file_chunks(file_id, state);
CREATE INDEX idx_samples_exp_id    ON samples(experiment_id);
CREATE INDEX idx_samples_role      ON samples(role, experiment_id);
CREATE INDEX idx_raw_responses_acc ON raw_responses(accession, source);
CREATE INDEX idx_discrepancies_exp ON discrepancies(experiment_id, resolved);
```

---

## 4. Máquina de estados de arquivo

Transições válidas. Cada transição é uma linha nova em `file_events`, nunca UPDATE destrutivo.

| De          | Para        | Gatilho                        |
| ----------- | ----------- | ------------------------------ |
| pending     | queued      | enfileirado para download      |
| queued      | downloading | worker pegou                   |
| downloading | verifying   | bytes completos                |
| downloading | failed      | erro de rede/servidor          |
| verifying   | done        | checksum global confere        |
| verifying   | failed      | checksum não confere           |
| failed      | retrying    | dentro do limite de tentativas |
| retrying    | downloading | nova tentativa                 |
| failed      | abandoned   | tentativas esgotadas           |
| done        | stale       | reverificação detectou mudança |
| stale       | verifying   | revalidação disparada          |

---

## 5. Contrato de resume

Entrada: um `File` com `path_part` existente e grade `file_chunks`.

Algoritmo:

1. `stat` no `.part` → tamanho real em disco.
2. Determinar ponto seguro: maior offset cujo chunk está `verified` na grade E ≤ tamanho em disco.
3. Truncar o `.part` ao ponto seguro (descarta cauda não verificada).
4. Requisitar `Range: bytes=<ponto_seguro>-`.
5. Se resposta for `200` (servidor ignorou Range): erro `ErrRangeUnsupported`, não gravar.
6. Se `206`: gravar a partir do ponto seguro, calculando BLAKE3 por chunk conforme avança, marcando cada chunk `verified` ao fechar.
7. Ao completar: validar arquivo inteiro contra `md5_expected`/`sha256` da fonte. Se confere, renomear `.part` → final, status `done`.

Invariante: após qualquer interrupção e retomada, o checksum global final sempre confere. Garantido por teste de propriedade (matar em offset aleatório N vezes).

Se a fonte expõe ETag/Last-Modified, enviar `If-Range` para detectar arquivo trocado entre runs.

---

## 6. Funil de verificação

```
verify(file):
    if baseline existe (last_verified_at, verified_size/mtime/inode):
        if stat atual == baseline e modo != deep:
            return OK   # gate barato, não lê conteúdo
    abrir arquivo, ler em buffer de 1MB
    calcular BLAKE3 por chunk numa passada
    comparar cada chunk com file_chunks.blake3
    se todos batem: atualizar last_verified_at + baseline; return OK
    senão: marcar chunks divergentes; return lista de chunks corrompidos
```

Modos: `--trust-mtime` (só gate), `--deep` (sempre lê conteúdo).

Scrub periódico: seleciona arquivos por `last_verified_at` ascendente, verifica até um teto de I/O por execução, cobre o acervo de forma amortizada ao longo de várias execuções.

Concorrência de verificação: default 1–2, configurável, sobe só em storage flash.

CLI `verify` standalone: aceita arquivo solto, hash esperado por flag (`--expected`) ou arquivo de checksums. Modo stateless (sempre lê conteúdo, sem baseline).

---

## 7. Writer goroutine

```
workers → canal de eventos (cada evento + chan error) → writer → SQLite
```

Writer único serializa escritas. Worker envia evento e espera ack no `chan error`. Erro de escrita (disco cheio, constraint) propaga e aborta o pipeline.

Shutdown: cancelar context → parar de aceitar eventos → drenar canal → commit final → fsync → sair.

Writes em batch: agrupar por contagem E tempo (ex. 100 eventos ou 50ms, o que vier primeiro), com commit forçado no cancelamento. Tamanho exato a calibrar.

---

## 8. Interfaces públicas (assinaturas)

```go
type Store interface {
    SaveExperiment(ctx context.Context, exp model.Experiment) error
    SaveSample(ctx context.Context, s model.Sample) error
    SaveFile(ctx context.Context, f model.File) error
    SaveChunks(ctx context.Context, fileID string, chunks []model.Chunk) error
    UpdateFileStatus(ctx context.Context, id string, status model.Status, reason string) error
    UpdateChunkState(ctx context.Context, fileID string, idx int, state string) error
    GetPendingFiles(ctx context.Context) ([]model.File, error)
    GetChunks(ctx context.Context, fileID string) ([]model.Chunk, error)
    SaveRawResponse(ctx context.Context, r model.RawResponse) error
    SaveDiscrepancy(ctx context.Context, d model.Discrepancy) error
    FilesForVerification(ctx context.Context, limit int) ([]model.File, error)
}

type Source interface {
    Fetch(ctx context.Context, accession string) ([]model.Sample, error)
    Download(ctx context.Context, samples []model.Sample) error
}

type Profile interface {
    Type() string
    ValidRoles() []string
    RequiresControl() bool
    LinkStrategy() LinkStrategy
    Validate(s model.Sample) []ValidationIssue
}

type Logger interface {
    Debug(msg string, fields ...any)
    Info(msg string, fields ...any)
    Warn(msg string, fields ...any)
    Error(msg string, fields ...any)
}
```

Construção via functional options:

```go
store, err := colligo.Open("study.db",
    colligo.WithWAL(true),
    colligo.WithLogger(logger),
)

enc := encode.New(store,
    encode.WithConcurrency(8),
    encode.WithRateLimit(5),
    encode.WithRetry(5, retry.Exponential),
)
```

Iteração sobre estudos grandes via `iter.Seq2` (Go recente), não slice materializado.

---

## 9. Erros tipados

```go
type ChecksumError struct{ ... }
type SourceError struct{ ... }
type TypeMismatchError struct{ ... }
type NotFoundError struct{ ... }
type AuthError struct{ ... }
type RangeUnsupportedError struct{ ... }
```

Inspecionáveis via `errors.As`.

---

## 10. Estratégia de erro de rede

| Erro                      | Estratégia                                |
| ------------------------- | ----------------------------------------- |
| Timeout / 5xx             | Retry com backoff exponencial + jitter    |
| 429 / 503 com Retry-After | Honrar o header acima do backoff próprio  |
| Checksum mismatch (chunk) | Rebaixar só o chunk                       |
| 200 quando esperava 206   | Erro permanente (`RangeUnsupportedError`) |
| 404                       | Falha permanente                          |
| 401 / 403                 | Falha permanente, avisa usuário           |

Backoff: `min(2^N, 300s) + jitter`. Jitter evita retry sincronizado.

Rate limiter compartilhado por chave de API (NCBI: um limiter único por API key, não um por goroutine).

---

## 11. Pools de concorrência

```
Metadata pool  → 20–50 goroutines, rate limitado por domínio
Download pool  → 4–8 goroutines, limite de conexões por domínio
```

Limites por domínio (default, configurável): encodeproject.org 4, ebi.ac.uk 4, ftp.ncbi.nlm.nih.gov 2.

HTTP: `Accept-Encoding: identity` para arquivos já comprimidos (.gz, BAM); reuso de conexão via `MaxIdleConnsPerHost`. Cópia com `io.CopyBuffer` de 1MB. Conteúdo de arquivo nunca em memória.

---

## 12. Configuração (TOML + Env + Flags)

Precedência: defaults < TOML < env < flags.

```toml
[study]
name = "meu_estudo"
db   = "study.db"

[sources.ncbi]
email      = "pesquisador@instituicao.br"
rate_limit = 3

[sources.encode]
rate_limit = 5

[download]
workers    = 8
retry_max  = 5
chunk_size = "8MB"
resume     = true

[cache]
experiment_metadata_ttl = "30d"
file_status_ttl         = "1d"
genome_metadata_ttl     = "90d"

[experiments.ENCSR123ABC]
type      = "chipseq"
condition = "H3K27ac_HCT116"
organism  = "human"
paired    = true
source    = "encode"
```

Env: `COLLIGO_NCBI_API_KEY` (eleva rate limit NCBI de 3→10 automaticamente), `COLLIGO_DB_PATH`, `COLLIGO_LOG_LEVEL`.

Como lib, segredos via options (`entrez.WithAPIKey(...)`); a lib não opina na origem do segredo.

Schema TOML exaustivo por tipo/fonte a completar ao implementar cada perfil.

---

## 13. Requisitos transversais

- Paths armazenados relativos à raiz do estudo, nunca absolutos (mover a pasta não pode quebrar resume).
- Sanitizar nome de arquivo vindo da API (não escrever fora do diretório do estudo).
- `.part` e arquivo final no mesmo filesystem (rename atômico).
- Pré-checar espaço em disco (`statfs`) antes de transferência longa.
- Validação Fase 1 (local, zero I/O) antes de qualquer chamada de rede.
- `-race` em todo teste de concorrência.

---

## 14. Testes

Servidor HTTP fake por fonte com fixtures JSON reais commitadas (offline, determinístico). Para download, servidor fake servindo arquivos pequenos com BLAKE3/MD5 conhecidos.

Injeção de falha via `httptest`: Range ignorado (200), Content-Length errado, corpo truncado, reset no meio, 429 com Retry-After.

Cobrir: parsing por fonte, detecção de discrepância, transições de estado, resume a partir de offset salvo, retry por tipo de erro, warnings, e o teste de propriedade do resume.
