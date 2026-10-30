# Colligo — Testes

Documento normativo. O que garante que um bug oculto de colaborador não chega ao usuário.

Premissa: revisão humana não escala e não é confiável para lógica de parsing de API que ninguém além do autor conhece. O que escala é uma suíte que o código do colaborador precisa **passar**, não uma que ele precisa escrever.

---

## 1. Conformidade de adapters

O core publica `sourcetest`. O colaborador não escreve os testes de invariante.

```go
func TestGENCODE(t *testing.T) {
    sourcetest.Run(t, sourcetest.Config{
        New:       func() colligo.Source { return gencode.New() },
        Fixtures:  "testdata/synthetic",
        Accession: "GENCODE_44_GRCh38",
    })
}
```

O que `sourcetest.Run` assere, sem o colaborador poder desligar:

| Invariante                       | Verificação                                                        |
| -------------------------------- | ------------------------------------------------------------------ |
| Cancelamento respeitado          | ctx cancelado antes da chamada → `ctx.Err()`, zero efeitos          |
| Determinismo                     | mesmo fixture 2× → saída byte-idêntica após serialização           |
| Sem cliente HTTP global          | `RoundTripper` injetado; dial real → `t.Fatal`                      |
| Rate limit honrado               | clock falso; N chamadas com limite 1/s levam ≥ N−1s                 |
| Roles válidos                    | todo `Sample.Role` ∈ `Profile.ValidRoles()`                         |
| Nome seguro                      | fixtures hostis (ver §2) → rejeitado ou saneado sob a raiz          |
| Erros tipados corretos           | 404/401/429/503/412 → `errors.As` bate; 429 honra `Retry-After`     |
| Raw persistido antes do parse    | parse que falha ainda deixa auditoria                               |
| Sem vazamento de goroutine       | `goleak` ao fim de cada subteste                                    |
| Sem escrita em disco             | FS instrumentado; qualquer `open` para escrita → `t.Fatal`           |
| Sem acesso a `Store`             | garantido pela assinatura; adapter não recebe `Store`               |
| Sentinela não vaza               | valor sentinela nos fixtures; grep em todo output do adapter        |
| Formato declarado é conhecido    | `Artifact.Format` ∈ registro de `format/`                            |
| Contagens declaradas coerentes   | `ReadCount`, `BaseCount` ≥ 0; `BaseCount` ≥ `ReadCount` quando ambos presentes |
| Metadado sem download            | `Fetch` e `Resolve` nunca pedem corpo de arquivo de dado             |

`storetest` e `profiletest` seguem o mesmo padrão. `storetest` é pré-requisito de qualquer implementação alternativa de `Store`.

**`sourcetest` é API pública.** Adicionar asserção quebra adapters existentes, portanto é evento de semver, decidido conscientemente.

### Fixtures hostis, sempre incluídos

Todo adapter é testado contra respostas contendo:

```
../../etc/passwd
..\..\windows\system32
nome\x00nulo
nome/com/separador
<255+ bytes>
аrquivo.fastq.gz          (а cirílico, homóglifo)
CON, NUL, PRN             (nomes reservados Windows)
arquivo .fastq.gz         (espaço à esquerda e à direita)
-rf                       (parece flag)
```

Nenhum desses pode produzir escrita fora da raiz do estudo, e nenhum pode fazer o adapter entrar em pânico.

---

## 2. Fixtures

Duas categorias, nunca misturadas.

### `testdata/synthetic/` — versionada

Mesma **forma** da resposta real, valores inventados. É o que roda no CI e para qualquer colaborador. Contém as sentinelas de vazamento.

Para os validadores de formato, o corpus sintético é produzido por um gerador versionado, `internal/corpus`: `go test ./internal/corpus -record` grava os arquivos e um `.meta.json` irmão com gerador, tamanho, SHA-256, data de gravação e o que o gerador sabe que escreveu — contagem de registros e bases, e quais afirmações devem falhar. Sem a flag, o teste falha se um arquivo divergir do gerador ou se aparecer arquivo que o gerador não produz. O esperado nunca vem de um parser: `check` sobre o diretório e o verificador Python são conferidos contra ele. O gerador comprime com `klauspost/compress`, fixado no `go.sum`: a saída do `compress/gzip` da stdlib muda entre versões do Go, e o corpus precisa dos mesmos bytes em qualquer toolchain.

### `testdata/real/` — nunca versionada

Capturas reais. Fica na máquina de quem tem acesso. Bloqueada por `.gitignore` e por check de CI que falha se algum arquivo sob esse caminho estiver rastreado.

Para regravar o corpus depois de mudar o gerador ou a versão de um extrator:

```
go test ./internal/corpus -record
git diff --stat testdata/synthetic
```

Revise o diff antes de comitar: arquivo que mudou sem o gerador ter mudado é sinal de bug.

### Regras de gravação

- Fixture é **gravada, nunca escrita à mão**: `go test -record` bate na API real, escreve o arquivo e remove credenciais.
- Cada fixture tem `.meta.json` irmão: URL, data de captura, versão da ferramenta, hash. Sem isso, em dois anos ninguém sabe se o fixture reflete a realidade.
- Fixture nunca é editada depois de gravada. Mutação para teste acontece **no código do teste**, para o diff ficar visível na revisão.

### Testes que dependem de fixture real

Atrás da build tag `restricted`. Sem a tag, não compilam.

Com a tag e sem a fixture presente: **`t.Fatal`, não `t.Skip`.** Pular em silêncio é exatamente o erro silencioso que este projeto existe para evitar — um CI mal configurado passaria verde sem rodar nada.

Falha envolvendo fixture real imprime hash e offset. Nunca conteúdo.

---

## 3. Drift

O CI de PR é 100% offline e determinístico. Isso é incompatível com detectar mudança de API — a menos que os jobs sejam separados.

**Job semanal, separado:** regrava todos os fixtures contra as APIs reais, compara com o versionado, e abre issue na divergência com o diff estrutural (campos que apareceram, desapareceram ou mudaram de tipo), nunca com valores.

Para fontes restritas, o job roda na infra da instituição e nunca publica diff.

---

## 4. Fuzzing

Teste de contrato pega o que você previu. Fuzzing pega o resto. Um alvo por parser de fonte, mais um por validador de formato.

```go
func FuzzParseGEO(f *testing.F) { ... }
func FuzzValidateGzip(f *testing.F) { ... }
```

Seed com os fixtures sintéticos. Invariantes:

- Nunca entra em pânico
- Nunca devolve role fora de `ValidRoles()`
- Nunca produz caminho que escapa a raiz do estudo
- Alocação não cresce desproporcionalmente ao input
- Nunca devolve formato fora do registro

CI de PR: 60s por alvo. Job semanal: 4h por alvo. Corpus interessante versionado.

Este é o item de maior valor por linha escrita contra bug que ninguém imaginou.

---

## 5. Testes de propriedade

**Resume.** Matar o download em offset aleatório N vezes; o checksum final sempre confere. É o teste central do projeto.

**Árvore BLAKE3.** Para tamanho de arquivo aleatório: grupos de 8 MiB com soma dos tamanhos igual ao total, offsets contíguos, sem lacuna nem sobreposição, e raiz derivada dos CVs igual ao BLAKE3 do arquivo inteiro, conferido contra `b3sum`. O último grupo, e o arquivo de um grupo só, são onde o off-by-one vive.

**Leitura única.** Leitor instrumentado conta bytes lidos. Em `check` e em download sem interrupção, bytes lidos = tamanho do arquivo. Qualquer releitura falha o teste.

**Fatos são funções puras.** Mesmo conteúdo com nomes, caminhos e mtimes diferentes → mesmos fatos, byte a byte. Mudar a versão de um extrator invalida só os fatos daquele `kind`.

**Máquina de estados.** Sequências aleatórias de eventos; só transições da tabela da SPEC §4 persistem. O teste lê o mapa de transições — o teste *é* a spec, não uma cópia dela.

**Extratores equivalentes.** Quando houver extrator externo, para todo arquivo do corpus sintético e de fuzz, fatos do extrator Go e do externo de mesma `extractor_version` são idênticos byte a byte. Divergência reduz automaticamente o arquivo ao menor prefixo que ainda diverge, para o relatório apontar o registro culpado.

**Redação.** Para todo tipo exportado do domínio, formatar com `%v`, `%s`, `%+v`, `%#v` e `json.Marshal`, e afirmar que a sentinela não aparece. Por reflexão sobre o pacote, para que um tipo novo entre no teste automaticamente.

**Idempotência de `files.id`.** Fetch duas vezes o mesmo fixture → mesmo id, sem linha duplicada.

**Manifesto e atestação.** Manifesto exportado e reatestado contra o próprio acervo sempre confere. Corromper um grupo conhecido → atestação aponta exatamente aquele índice.

---

## 6. Filesystem e ambiente hostis

Wrapper de FS injetando:

- `ENOSPC` na escrita
- `EIO` na leitura
- short write
- `fsync` que retorna erro
- `rename` que falha
- mtime que anda para trás
- inode que muda entre stats

É onde "dado parcial tem valor" se prova ou se revela slogan. O caso crítico: `ENOSPC` precisa gravar o offset seguro antes de propagar.

Ambiente: relógio para trás, permissão frouxa no banco, PRAGMA que não surte efeito, `.part` e final em filesystems diferentes.

---

## 6.1 Crash em qualquer ponto

O wrapper de FS numera cada operação de escrita, `fsync`, `rename` e commit de banco de uma execução de referência de `get` e de `fetch`. Para cada número N, a suíte roda de novo, mata o processo com `SIGKILL` exatamente na operação N, descarta o que não recebeu `fsync` (simulando perda de cache do sistema operacional), retoma, e confere:

- Nenhum arquivo com nome final sem todas as afirmações aprovadas.
- Nenhum grupo `verified` cujo conteúdo em disco não bate com o CV.
- Nenhum evento no banco apontando para estado que o disco não sustenta.
- O arquivo final, ao terminar, é idêntico ao da execução sem falha.

Custo alto: roda no job semanal completo e numa amostra aleatória de pontos em todo PR.

## 6.2 Modelo formal

`spec/resume.tla` especifica o protocolo de resume, rename e máquina de estados. O model checker TLC roda no CI com parâmetros pequenos (3 grupos, 2 falhas, 2 reinícios) em todo PR que toca `downloader/`, `store/` ou `verifier/`. As invariantes do modelo são as mesmas de §6.1: o modelo prova para o protocolo, o crash-testing confere que o código segue o protocolo.

## 6.3 Certificado e verificador independente

Para todo arquivo do corpus sintético e dos mutantes, `check --certificate` e `tools/verify_cert.py` são executados; o verificador precisa concordar com cada hash e contagem do certificado, e reprovar todo certificado adulterado, inclusive o `passed` das afirmações de um arquivo inválido.

## 6.4 Concorrência adaptativa

Servidor fake com capacidade configurável, latência e 429 acima de um limite de conexões. A concorrência adaptativa precisa convergir para perto da capacidade, nunca exceder o teto configurado, honrar `Retry-After` e terminar sem erro permanente. Clock falso, determinístico.

---

## 7. Mutantes: benchmark de falhas silenciosas

Versionado no repositório, reprodutível por qualquer um. Não é otimização — é a evidência de que "robusto" é medida, não adjetivo. É também o ativo publicável do projeto: nenhum benchmark mede quais falhas silenciosas cada ferramenta de bioinformática deixa passar.

O pacote `mutants/` gera, a partir de arquivos sintéticos válidos, versões corrompidas de propósito. Cada classe de mutante declara qual modo de falha de `RELIABILITY.md` representa.

| Classe                         | Mutação                                                        | Modo |
| ------------------------------ | -------------------------------------------------------------- | ---- |
| `truncate`                     | corta em offset aleatório, inclusive no meio de membro gzip    | M4   |
| `truncate-record`              | remove o último registro inteiro, arquivo continua bem formado | M6, M15 |
| `html-body`                    | página HTML de erro com a extensão do formato                  | M3   |
| `flip-byte`                    | um byte alterado em offset aleatório                           | disco |
| `shift`                        | bytes inseridos ou removidos no meio                           | disco |
| `short-mate`                   | R2 com reads encurtados, estrutura válida                      | M15  |
| `pair-desync`                  | um read removido só de R2                                      | M5   |
| `pair-swap`                    | R2 de outra amostra                                            | M5   |
| `chrom-rename`                 | `chr1` → `1` na anotação                                       | M11  |
| `release-swap`                 | GTF de outro release com o mesmo nome                          | M1   |
| `superseded`                   | fonte publica checksum diferente na mesma URL                  | M2   |
| `suppressed`                   | fonte marca o run como suprimido                               | M16  |
| `chemistry-swap`               | R1 de 10x v2 com metadado declarando v3                        | M17  |
| `matrix-colswap`               | duas colunas da matriz com nomes trocados; uma coluna removida | M18  |
| `duplicate-run`                | mesmo conteúdo sob duas accessions                             | M19  |

Dois usos:

- **Suíte do projeto.** Toda mutação de toda classe que o Colligo declara detectável é detectada, com o código de saída certo. Uma classe nova de mutante entra junto com a defesa que a pega.
- **Benchmark.** A mesma matriz roda contra outras ferramentas: `gzip -t`, `samtools quickcheck`, validadores de FASTQ, `md5sum -c` contra o MD5 da fonte, iSeq, `wget -c`, `curl -C -`, `prefetch`. Saída: tabela classe × ferramenta com detectou ou não, e tempo até detecção.

Matriz de transporte, mantida: `{Range ignorado, corpo truncado, reset no meio, 429, 412, ENOSPC}` × `{Colligo, wget -c, curl -C -, prefetch}`, com taxa de resume bem-sucedido, bytes retransmitidos e throughput.

Roda contra servidor local, sem rede externa. Saída em tabela versionada, para que regressão apareça em diff.

---

## 7.1 Desempenho

Velocidade é requisito medido, não impressão. Benchmark versionado, rodando na máquina de referência declarada no próprio resultado (CPU, disco, sistema de arquivos).

| Medida | Piso de comparação | Meta |
| ------ | ------------------ | ---- |
| `check` completo de `.fastq.gz` | gunzip em Go puro (`klauspost/compress`), mesmo arquivo e máquina | ≤ 110% do piso; `zcat \| wc -l` medido e reportado |
| `check` completo de BAM | `samtools view -c` | ≤ 110% do piso |
| Hash de arquivo não comprimido | `b3sum` | ≤ 120% do piso |
| Segunda execução, 10 mil arquivos inalterados | — | ≤ 5 s em SSD |
| Alocações por registro no parser de FASTQ | — | 0 |
| `get` com verificação completa | `curl` puro, mesmo arquivo e servidor local | ≤ 105% do piso |

Perfis de CPU e memória (`pprof`) de cada medida são versionados junto com a tabela, para que regressão apareça em diff. Uma mudança que piora uma medida em mais de 5% falha o job semanal com o perfil anexado.

A troca de `compress/gzip` por `klauspost/compress/gzip` só entra com o resultado deste benchmark mostrando ganho.

---

## 7.2 Recursos e limites

O `Compute` é medido contra entrada hostil ou grande, para que uma regressão de memória quebre a suíte em vez de aparecer num servidor.

| Teste                                           | Entrada                                                                    | O que afirma                                                                             |
| ----------------------------------------------- | -------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| gzip de razão enorme                            | 2 GiB de zeros, ~2 MiB comprimidos, gerados em streaming                   | termina, `uncompressed_bytes` exato, pico de heap ≤ 32 MiB acima da linha de base        |
| 20 milhões de reads curtos (`COLLIGO_SLOW=1`)   | ~1,1 GiB gerados em streaming, comprimentos 20 a 24                        | histograma, `read_ends`, MD5 e contagens exatos; pico ≤ 32 MiB; último quarto não cresce |
| arquivo que encolhe ou cresce durante a leitura | truncado ou acrescido quando metade foi lida                               | tamanho e hashes são dos bytes efetivamente lidos; o resultado não acusa a mudança       |
| alocações por MiB lido                          | FASTQ puro e FASTQ gzip, inclinação entre uma entrada pequena e uma grande | ≤ 8 alocações e ≤ 16 KiB alocados por MiB adicional                                      |
| leitura única                                   | `CheckFile` e `CheckAll`, medidos por `rchar` de `/proc/self/io` (Linux)   | cada byte lido exatamente uma vez; `Discover` lê no máximo 64 KiB por arquivo            |

Os limites partem do piso estrutural, não do que a máquina de hoje faz: o anel de leitura tem 8 buffers de 1 MiB e o descompressor mais o extrator ficam abaixo de 1 MiB, então o pico medido fica entre 8 e 13 MiB e o teto de 32 MiB deixa folga para o coletor de lixo. Reter mais de 1,5% da saída descomprimida (32 MiB de 2 GiB) já passa do teto. A inclinação de alocações medida é zero dentro do ruído (no máximo uma alocação e algumas centenas de bytes por MiB); os limites pegam uma alocação por registro (milhares por MiB) ou um buffer novo por pedaço (1 MiB por MiB).

`COLLIGO_SLOW=1` liga os testes que levam minutos (20 milhões de reads: cerca de 9 s sem `-race` e 50 s com). Sem a variável eles pulam e a mensagem diz como ligar. O CI não a define.

---

## 8. Regras mecânicas

Nenhuma delas depende de alguém lembrar.

| Regra                              | Ferramenta                                          |
| ---------------------------------- | --------------------------------------------------- |
| Core não importa `source/*`        | teste com `x/tools/go/packages`, ou `depguard`       |
| Porta leve não importa `store`     | mesmo teste de grafo de import                       |
| `string` crua onde cabe `secret.*` | analisador `go/analysis` próprio                     |
| Sem `init()` com efeito colateral  | lint customizado                                    |
| Superfície pública estável         | `apidiff` contra a última tag                        |
| Corrida                            | `go test -race -count=2 ./...`                       |
| Vazamento de goroutine             | `goleak` em todo teste de concorrência               |
| Segredo no diff                    | `gitleaks` em pre-commit e no CI                     |
| Fixture real rastreada             | `git ls-files` casado contra padrões proibidos        |
| Cobertura de asserção real         | `gremlins` (mutation), trimestral, só no core        |
| Sem host não configurado           | dialer instrumentado, em toda a suíte                |

`-count=2` pega estado vazando entre execuções. Mutation score é a única métrica que responde "minha suíte pegaria um bug oculto?" — cobertura só diz quanto código rodou.

---

### Ferramentas externas

Alguns testes comparam o Colligo com programas que não são nossos: o verificador `tools/verify_cert.py` (Python com o pacote `blake3`) e `b3sum`. Teste que depende deles nunca passa em silêncio sem ter rodado:

| Situação | Resultado |
| -------- | --------- |
| `COLLIGO_PYTHON` definida e o interpretador não importa `blake3` | falha |
| `COLLIGO_REQUIRE_PYTHON=1` e nenhum Python com `blake3` | falha |
| nada definido e sem Python com `blake3` | pula, e a mensagem diz como transformar o pulo em falha |
| `COLLIGO_REQUIRE_B3SUM=1` e sem `b3sum` | falha |
| sem `b3sum` e sem a variável | a comparação não roda e o teste registra isso em log |

O CI define `COLLIGO_REQUIRE_PYTHON=1` e `COLLIGO_REQUIRE_B3SUM=1`. O benchmark de desempenho é opt-in por `COLLIGO_PERF=1`, porque mede velocidade e não correção. Os testes de recurso que levam minutos são opt-in por `COLLIGO_SLOW=1`, porque custam tempo e não correção a mais no dia a dia.

### Variáveis `COLLIGO_*`

| Variável | Efeito |
| -------- | ------ |
| `COLLIGO_PYTHON` | caminho do interpretador Python com `blake3` usado pelos testes de `tools/verify_cert.py`; se definida e o interpretador não importa `blake3`, falha |
| `COLLIGO_REQUIRE_PYTHON` | `1` transforma a ausência de Python com `blake3` em falha, em vez de pulo |
| `COLLIGO_REQUIRE_B3SUM` | `1` transforma a ausência de `b3sum` em falha, em vez de registro em log |
| `COLLIGO_PERF` | qualquer valor não vazio liga `TestPerformanceFloor`; sem ela o teste pula |
| `COLLIGO_PERF_DIR` | diretório onde o benchmark guarda e reaproveita os `.fastq.gz` gerados; sem ela usa um diretório temporário do teste e gera de novo a cada execução |
| `COLLIGO_SLOW` | qualquer valor não vazio liga os testes de recurso que levam minutos (§7.2) |

### Benchmark de desempenho

```
COLLIGO_PERF=1 COLLIGO_PERF_DIR=/caminho/com/espaco go test ./facts -run TestPerformanceFloor -v
```

O teste gera um `.fastq.gz` com 1 GiB de FASTQ descomprimido (reads de 100 bases, semente fixa), mede três vezes (melhor tempo) o gunzip em Go puro, `zcat | wc -l` e `Compute` com hashes, e imprime os três tempos e as razões. Falha se o `Compute` passar de 110% do piso em Go puro; a razão contra `zcat | wc -l` só é reportada. Exige `zcat` no `PATH`. `go test ./facts -bench ComputeFastqGz` mede o mesmo `Compute` sobre um arquivo gerado com 256 MiB descomprimidos como benchmark comum do Go, sem meta.

---

## 9. O que o CI roda

**Em todo PR:** nome do branch, título do PR e mensagem de cada commit (`.github/check-names.sh`, que também se autotesta), build Linux, `vet`, lint, analisador de tipos `secret`, `-race -count=2`, `sourcetest` de todas as fontes públicas, suíte de mutantes do Colligo, fuzz 60s por alvo, `apidiff`, `gitleaks`, check de fixture rastreada, check de grafo de import e da regra das duas portas.

**Semanal:** drift de fixtures, fuzz 4h por alvo, benchmark de mutantes contra ferramentas externas, benchmark de desempenho.

**Trimestral:** mutation testing.

**Nunca no CI:** qualquer teste com build tag `restricted`.
