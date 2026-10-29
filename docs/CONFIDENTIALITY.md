# Colligo — Confidencialidade

Documento normativo. Descreve as pontas por onde dado restrito pode escapar e o mecanismo que fecha cada uma.

Princípio único deste documento: **proteção por tipo, não por disciplina.** Todo mecanismo aqui é estrutural — o caminho errado não compila, não abre, ou não roda. Nenhum mecanismo depende de alguém lembrar de redigir algo.

**Quando entra.** Os tipos da §3 e o analisador da §3.1 entram na Fatia 0, porque retrofitar tipos é reescrever. O restante do regime — §5.2 a §5.12 e a tabela da §6 — entra na Fatia 6, junto com o primeiro adapter de fonte restrita. Antes disso não existe estudo restrito para proteger, e construir o regime inteiro atrasaria a primeira entrega útil sem proteger nada. Ver `DESIGN.md`, "Confidencialidade entra pelos tipos".

---

## 1. Modelo de ameaça

O adversário não é malicioso. É o acidente.

Casos que este documento previne:

- Um `.db` de estudo com dado de paciente sincronizado para nuvem pessoal.
- Uma stack trace com caminho de arquivo contendo prontuário, colada num issue público.
- Um manifesto exportado achando que só continha hashes, contendo nomes de arquivo hospitalar.
- Uma fixture de teste real commitada por engano.
- Um token de dbGaP em `argv`, visível a qualquer usuário da máquina via `ps`.
- Uma credencial num TOML versionado.

Fora de escopo: adversário com acesso root à máquina, adversário com acesso ao banco e ao salt simultaneamente, e análise de tráfego de rede. Colligo não é ferramenta de criptografia.

---

## 2. Classificação

Toda fonte tem classificação declarada. Duas classes.

| Classe       | Significado                                                        |
| ------------ | ------------------------------------------------------------------ |
| `public`     | Dado publicamente acessível. Sem restrição de retenção ou export.  |
| `restricted` | Acesso controlado: dbGaP, dado institucional, dado de paciente.    |

**A declaração é obrigatória e falha fechado.** Uma fonte configurada sem `classification` causa erro de carregamento nomeando a fonte. Não há default. Não há inferência.

```toml
[sources.gencode]
classification = "public"

[sources.dbgap]
classification = "restricted"
```

Um estudo que contém qualquer fonte `restricted` é um **estudo restrito**, e o modo restrito se aplica ao banco inteiro — não por linha. Motivo: correlação. Saber que o paciente X está no mesmo estudo que o paciente Y já é informação.

A classificação do estudo é gravada em `study_meta` na criação e é **imutável**. Um estudo público não pode ser promovido a restrito, porque os dados já foram gravados em claro; a promoção seria gambiarra. Erro instrui a criar estudo novo.

**Artefato derivado herda a classificação mais estrita entre seus pais.** Um GTF fundido de GENCODE (`public`) com um arquivo `restricted` é `restricted`. Enforçado no registro do derivado, não no uso.

---

## 3. Tipos que não vazam

Três tipos no core. Nenhum dado sensível trafega como `string` nua em nenhum ponto do sistema.

### `Secret`

Envelopa credencial. Não implementa nada que a imprima.

```go
type Secret struct{ v string }

func (Secret) String() string          { return "[REDACTED]" }
func (Secret) GoString() string        { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }
func (s Secret) reveal() string        { return s.v }
```

`reveal` é não exportado. Só o pacote de transporte alcança o valor, no momento de montar o header. `%v`, `%s`, `%+v`, `log`, `json.Marshal` e `spew` todos produzem `[REDACTED]` por construção.

### `Name`

Envelopa nome de arquivo e caminho.

```go
type Name struct {
    raw        string
    opaque     string
    restricted bool
}

func (n Name) String() string
func (n Name) Raw(fs FSAccess) string
```

A classificação viaja **dentro** do `Name`. Um `Name` só é construído pelo estudo — que conhece classificação e salt — ou pela porta leve, que não tem estudo e constrói sempre `Name` público. Não existe estado global consultado em `String()`.

`String()` devolve `raw` se `restricted` é falso e `opaque` se é verdadeiro.

`Raw()` é a única porta para o valor real e exige um `FSAccess`: token cujo construtor é não exportado e vive no pacote de filesystem interno. Export, log e manifesto não conseguem obter um `FSAccess`, portanto não conseguem chamar `Raw()` — o erro é de compilação, não de runtime. Versão anterior desta seção dizia que `Raw()` "retorna erro em contexto de export ou log"; isso não é detectável em runtime sem parâmetro explícito, e foi substituído pelo token.

### `Endpoint`

Envelopa URL. `String()` em estudo restrito devolve `scheme://host/[REDACTED]`. Query string **nunca** aparece, em nenhuma classe, porque query é onde credencial se esconde.

### Consequência

O logger não aceita `string` para campos de caminho, URL ou credencial. A assinatura exige os tipos:

```go
log.Warn("download falhou", logf.Name("file", f.Name), logf.Endpoint("url", f.URL))
```

Passar `string` cru para um campo desses não compila. É isto que faz o mecanismo ser estrutural.

### 3.1 O que o compilador não pega

A garantia acima só vale se não existir porta lateral. `logf` não tem campo `String` genérico, mas nada impede `fmt.Sprintf("%s", raw)` seguido de `logf.Text`, nem um `string` com caminho atravessando código que não usa `logf`. O fechamento é um analisador `go/analysis` próprio, rodando no CI:

- Proíbe conversão de `secret.Name`, `secret.Endpoint` e `secret.Secret` para `string` fora de `internal/fsaccess` e `transport/`.
- Proíbe campos e parâmetros `string` com nome que casa `(?i)(path|url|uri|file|name|token|secret|key)` em pacotes do core, exceto onde anotados por tipo.
- Falha o build, não emite aviso.

Tipo mais analisador é a garantia. Tipo sozinho é intenção.

---

## 4. Identificadores opacos

Estudo restrito precisa de identificadores estáveis para auditoria sem expor nomes.

- Na criação do estudo, gera-se um **salt** de 32 bytes de `crypto/rand`, gravado em `study_meta`.
- `opaque(nome) = hex(BLAKE3(salt || nome))[:16]`.
- Estável dentro do estudo: o mesmo arquivo tem sempre o mesmo id opaco, então diffs de manifesto funcionam.
- Não correlacionável entre estudos: salts diferentes.
- Não reversível sem o salt.

**O salt nunca sai do banco.** Não vai para manifesto, export, log, relatório de bug ou backup de configuração. Toda rotina de export tem teste que falha se o salt aparecer no output.

---

## 5. As pontas, uma por uma

### 5.1 Banco de dados

`raw_responses` é a maior superfície: guarda respostas de API na íntegra.

| Classe       | Comportamento                                                       |
| ------------ | ------------------------------------------------------------------- |
| `public`     | Corpo gravado comprimido (zstd), TTL padrão 90d.                    |
| `restricted` | **Corpo não é gravado.** Grava-se `body_hash`, tamanho e timestamp. |

Enforçado na camada `store`, consultando a classificação do estudo. O chamador não tem parâmetro para desligar. Um adapter malicioso ou bugado não consegue persistir corpo restrito porque a decisão não é dele.

A auditoria sobrevive: hash e tamanho provam que a resposta foi a mesma entre duas execuções, que é o que a auditoria precisa responder.

### 5.2 Permissões de arquivo

Na abertura de estudo restrito:

- Plataforma POSIX obrigatória: Linux, macOS ou WSL. Estudo restrito em Windows nativo é recusado — as permissões abaixo não têm equivalente direto, e OneDrive é a pasta sincronizada padrão do sistema.
- Arquivo do banco: `0600`. Diretório do estudo: `0700`.
- Permissão mais frouxa → **erro**, não aviso. Override explícito `--allow-insecure-permissions`, que grava evento em `tool_runs`.
- WAL e SHM herdam o modo do banco; verificado após a primeira escrita, porque o SQLite os cria.
- Estudo público: aviso apenas.

### 5.3 Diretórios de sincronização

Na abertura e no `doctor`, o caminho do estudo é comparado contra padrões conhecidos de sincronização de nuvem (`Dropbox`, `Google Drive`, `OneDrive`, `iCloud Drive`, `Sync`, `Nextcloud`, e o conteúdo de `COLLIGO_SYNC_DIRS`).

Estudo restrito dentro de um deles → **erro**. Override `--allow-synced-path`, registrado.

Não é paranoia: é o vetor mais comum de vazamento acidental de dado de pesquisa.

### 5.4 Credenciais

**Credencial em TOML é erro de carregamento.** O parser rejeita qualquer chave que casa com `(?i)(password|passwd|secret|token|api_?key|credential|bearer)` sob `[sources.*]`, nomeando a chave e a linha.

Origens permitidas, nesta ordem:

1. Variável de ambiente, nome declarado: `credential_env = "COLLIGO_DBGAP_TOKEN"`.
2. Comando externo: `credential_command = "pass show dbgap/token"`. Stdout capturado como `Secret`, stderr descartado, timeout de 10s.
3. Arquivo apontado por `credential_file`, que precisa ser `0600`.

**Nunca em `argv`.** Colligo não tem flag que aceite credencial. `ps` não revela nada.

**Nunca em query string.** Credencial vai em header. Se uma fonte só aceita credencial em query, ela é marcada `credential_in_query = true`, e nesse caso a URL nunca é persistida nem logada — grava-se URL com query removida mais `url_query_stripped = 1`, e a query é remontada da config no momento do request.

### 5.5 Logs

- Campos de caminho, URL e credencial exigem os tipos da §3.
- Estudo restrito: nível de log máximo é `info`. `debug` em estudo restrito → erro, porque debug imprime corpos de resposta.
- Nenhum log grava corpo de resposta em estudo restrito, em nenhum nível.

### 5.6 Erros e pânicos

- Erros tipados carregam `Name` e `Endpoint`, nunca `string`. Um erro embrulhado imprime redigido em qualquer profundidade de `%w`.
- `recover` no topo de todo worker: converte pânico em erro tipado, grava tipo e local, **descarta a mensagem original** em estudo restrito, porque mensagem de pânico costuma conter valores.
- `GOTRACEBACK` é forçado para `none` em estudo restrito, para não despejar valores de variáveis na saída.

### 5.7 Manifesto

O manifesto é feito para circular. É a ponta de maior risco.

| Campo               | `public` | `restricted`     |
| ------------------- | -------- | ---------------- |
| nome / caminho      | real     | id opaco         |
| URL de origem       | real     | omitido          |
| tamanho             | sim      | sim              |
| raiz e outboard     | sim      | sim              |
| checksum da fonte   | sim      | sim              |
| formato e custódia  | sim      | sim              |
| accession           | real     | id opaco         |
| salt                | nunca    | nunca            |

Manifesto de estudo restrito carrega `"classification": "restricted"` e `"redacted": true` no cabeçalho, para quem recebe saber que os nomes são opacos.

Um manifesto restrito continua **completamente funcional** para atestação: `colligo attest` compara hashes, e hashes não são sensíveis.

### 5.8 Export tabular

TSV/CSV/JSON de estudo restrito usa nomes opacos nas mesmas regras do manifesto. O sidecar `.custody.json` segue a mesma tabela.

Export de estudo restrito para caminho fora da raiz do estudo → aviso com o caminho de destino, e erro se o destino for diretório sincronizado.

### 5.9 Repositório

- `.gitignore`: `testdata/real/`, `*.local.toml`, `*.db`, `*.db-wal`, `*.db-shm`, `.colligo/`.
- Hook de pre-commit com `gitleaks` no diff.
- Check de CI: `git ls-files` casado contra padrões proibidos; qualquer arquivo sob `testdata/real/` rastreado **falha o build**.
- Nada específico de instituição em código: host, projeto, nome de campo interno e identificador de estudo controlado são configuração. Ver `CONTRIBUTING.md` §Adapter para fonte privada.

### 5.10 Testes

Duas categorias de fixture, nunca misturadas. Detalhe em `TESTING.md` §2.

- `testdata/synthetic/` — versionada. Mesma **forma** do real, valores inventados.
- `testdata/real/` — nunca versionada.

Teste que depende de fixture real fica atrás da build tag `restricted`. Sem a tag, não compila. Com a tag e sem a fixture, **falha** — não pula em silêncio, porque pular em silêncio é o erro silencioso que este projeto existe para evitar.

Falha de teste envolvendo fixture real imprime hash e offset, nunca conteúdo.

### 5.11 Rede

Colligo não tem telemetria, crash reporting, verificação de versão nem analytics. Isto é garantia, não intenção, e tem teste:

O `DialContext` do cliente HTTP é instrumentado em teste para registrar todo host contactado. A suíte falha se o conjunto de hosts não for subconjunto das fontes configuradas. Nenhum host adicional, nunca.

### 5.12 Relatório de bug

`colligo redact-report` produz saída segura para colar em público:

Inclui — versão da ferramenta, git SHA, Go, OS/arch, hash da config, classificação por fonte, PRAGMAs efetivos, contagem de arquivos por estado, tipos de erro das últimas 20 falhas, resultado do `doctor`.

Exclui — nomes, caminhos, URLs, accessions, corpos, salt, credenciais, e qualquer valor com entropia.

Formato estável, para que se possa fazer diff entre duas máquinas.

---

## 6. Invariantes verificáveis

Toda linha desta tabela é um teste. Se algum falhar, o build falha.

| Invariante                                                    | Como é testado                                       |
| ------------------------------------------------------------- | ---------------------------------------------------- |
| Salt não aparece em nenhum output                             | grep no output de todo comando de export             |
| Credencial não aparece em nenhum output                       | credencial sentinela de alta entropia; grep global   |
| Corpo restrito não chega ao banco                             | inspeção de `raw_responses` após fetch restrito      |
| `%v` de todo tipo do domínio é seguro                         | reflexão sobre os tipos exportados; formata e grep   |
| `Raw()` inalcançável fora do filesystem                       | analisador §3.1 e teste de compilação negativo       |
| Nenhum host além dos configurados                             | dialer instrumentado                                 |
| Permissão do banco restrito é `0600`                          | stat após abertura, incluindo WAL e SHM              |
| Manifesto restrito não contém nome real                       | manifesto de fixture com nome sentinela; grep        |
| `debug` em estudo restrito falha                              | teste de erro esperado                               |
| Fixture real nunca rastreada                                  | `git ls-files` no CI                                 |
| Derivado de pai restrito é restrito                           | registro de derivado misto                           |

A técnica da sentinela merece nota: injeta-se um valor único e improvável (`COLLIGO_CANARY_7f3a…`) como nome de arquivo, credencial e corpo de resposta nos fixtures de teste, e a suíte faz grep dele em **todo** artefato produzido — log, export, manifesto, relatório, banco. Um único vazamento em qualquer ponta aparece.

---

## 7. O que este documento não resolve

Ditos com clareza para não gerar falsa confiança:

- **Não criptografa nada em repouso.** Se o disco não é criptografado, o dado está em claro. Use criptografia de disco do sistema operacional.
- **Não impede exfiltração deliberada** por quem tem acesso ao banco e ao salt.
- **Não substitui aprovação de acesso.** Colligo não sabe se você tem direito ao dado.
- **Não anonimiza o conteúdo dos arquivos.** Um FASTQ é identificável por natureza; nenhum nome opaco muda isso. Colligo protege metadado e trilha de auditoria, não o dado primário.
- **Não protege contra o pipeline downstream.** Se sua próxima ferramenta escreve caminhos em log, o problema volta.
