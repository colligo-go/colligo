# Colligo — Convenções

Estilo da casa. Inviolável durante implementação. Curto de propósito.

---

## Código

**Sem comentários. Nenhum.** Nem em Go, nem em SQL, nem em TOML de exemplo. Nomes comunicam intenção. Se um trecho precisa de comentário para ser entendido, ele precisa ser reescrito, não comentado.

**Erros são valores e são tratados.** Nada de `_` engolindo erro fora de casos genuinamente irrelevantes. Erros de domínio são tipados (SPEC §10) e inspecionáveis via `errors.As`. Todo erro carrega `Severity()`.

**Caminho, URL e credencial nunca são `string`.** Use `secret.Name`, `secret.Endpoint`, `secret.Secret`. Isto não é preferência de estilo: é o mecanismo que impede vazamento. Código que passa `string` crua para campo de log não compila, e o analisador de `CONFIDENTIALITY.md` §3.1 reprova as portas laterais. Erros tipados seguem a mesma regra.

**Concorrência via `errgroup` com `SetLimit`.** Nunca `go func` solto. Um erro cancela os irmãos via context; espera-se todos. Todo código concorrente é testado com `-race`.

**`context.Context` é o primeiro parâmetro** de toda operação que faz I/O ou pode ser cancelada. Cancelamento salva progresso parcial antes de encerrar.

**Functional options** para construção pública. Uso mínimo tem defaults sensatos; controle fino é opcional.

**Sem `init()` com efeito colateral.** Fontes são registradas explicitamente via option. Sem estado global mutável, sem ordem de inicialização importando.

**Conteúdo de arquivo nunca em memória.** Streaming para disco, sempre. `io.CopyBuffer` com buffer de 1MB.

**Uma leitura por arquivo.** Tudo que se deriva do conteúdo — hashes, árvore, estrutura, contagens — sai da mesma leitura, distribuída em paralelo a uma goroutine por consumidor (SPEC §7). `io.MultiWriter` é proibido nesse caminho, porque serializa os consumidores. Extrator não abre arquivo nem faz seek. Ler duas vezes é bug.

**Caminho quente não aloca.** Parser e extrator operam sobre o buffer recebido. Alocação por registro é bug de desempenho e tem teste com `testing.AllocsPerRun`.

**Fato é função pura do conteúdo.** Extrator não lê nome, caminho, mtime nem config. Mudou o que ele calcula, sobe a versão.

**A porta leve não importa `store`.** Todo recurso novo se pergunta primeiro se funciona sem banco. Se funciona, nasce no núcleo e a porta pesada só o persiste.

**Adapters são puros.** Não recebem `Store`, não recebem caminho de disco, não abrem arquivo. Um adapter que precisa disso está resolvendo o problema errado.

**Stdlib primeiro.** Dependência externa só onde a stdlib é genuinamente insuficiente. As conscientes da v1: `modernc.org/sqlite`, um pacote BLAKE3, `golang.org/x/sync/errgroup`, `golang.org/x/time/rate`, um pacote zstd, um cliente FTP isolado. Qualquer adição precisa de justificativa.

---

## Banco

**Nunca escrever em tabela do Scribo.** `result_inputs` e `input_signals` são só leitura para o Colligo. `CONTRACT.md` é idêntico nos dois repositórios; o CI compara as cópias.

**Transições de estado nunca sobrescrevem. São linhas novas em `file_events`, sempre com `run_id`. O banco é o estado canônico; TSV e manifesto são exportação.

**Arquivo `done` nunca é sobrescrito.** Conteúdo diferente na mesma URL cria linha nova com `supersedes`. Não existe caminho de código que faça UPDATE destrutivo em `done`.

**Todas as escritas passam pelo writer goroutine.** Workers não escrevem direto no SQLite.

**PRAGMAs sempre, no DSN, e lidos de volta.** Nenhuma conexão abre sem `foreign_keys=ON`, journal mode resolvido pelo filesystem (`WAL` local, `DELETE` em rede), `synchronous=NORMAL`, `busy_timeout=5000` e `_txlock=immediate` — e sem confirmar que surtiram efeito. Abertura idêntica à do Scribo, conforme `CONTRACT.md` §2. Nunca `db.Exec("PRAGMA …")` num `*sql.DB`: é pool, e só uma conexão recebe.

**Migration roda em transação**, junto com o registro em `schema_migrations`. Metade de migration aplicada é estado que ninguém sabe reconstruir.

**Tabelas `STRICT`, timestamps em UTC RFC 3339.**

**A camada `store` decide sobre dado restrito, não o chamador.** `SaveRawResponse` consulta a classificação e grava corpo ou hash. Não há parâmetro para desligar.

---

## Testes

**Offline e determinístico.** Servidor HTTP fake com fixtures versionadas. Nenhum teste de PR toca a rede.

**Injeção de falha explícita.** Range ignorado, corpo truncado, reset, 429, 412, `ENOSPC`, `fsync` falhando — cada um tem teste próprio. É onde a robustez se prova.

**Todo adapter passa em `sourcetest`.** Sem exceção, incluindo adapters privados.

**Fixture é gravada, nunca escrita à mão.** Duas categorias: `testdata/synthetic/` versionada, `testdata/real/` nunca. Detalhes em `TESTING.md`.

**Teste que depende de fixture real falha quando ela falta.** Nunca pula em silêncio.

**Teste escrito junto com o código**, não depois. Exemplos executáveis onde fizerem sentido como documentação viva.

**A tabela de transições é uma variável, e o teste lê dela.** O teste é a spec, não uma cópia.

---

## Documentação

**GoDoc só onde acrescenta algo.** Em identificador exportado, e apenas quando diz o que o nome e a assinatura não dizem: um invariante, uma restrição de uso, um porquê não óbvio. GoDoc que repete o nome é ruído e sai. É a única forma de texto explicativo permitida no código; identificador não exportado não leva comentário.

**Idioma.** `docs/` em português. Código, GoDoc, nomes e mensagens de teste, mensagens de erro, diagnósticos e saída do CLI em inglês, como o README público.

**Decisão muda código e doc no mesmo commit.** SPEC e implementação não desincronizam.

**Onde cada coisa mora:** por quê em `DESIGN.md`, o quê em `SPEC.md`, defesa contra erro silencioso em `RELIABILITY.md`, proteção de dado restrito em `CONFIDENTIALITY.md`, como testar em `TESTING.md`, como contribuir em `CONTRIBUTING.md`, o que foi adiado em `BACKLOG.md`, o que já existe fora daqui em `RELATED.md`.

---

## Git

**`CLAUDE.md` é privado** (`.gitignore`). Não vai para o repositório.

**`.gitignore` obrigatório:** `testdata/real/`, `*.local.toml`, `*.db`, `*.db-wal`, `*.db-shm`, `.colligo/`.

**`gitleaks` em pre-commit.** Credencial vazada não se desvaza.

**Nada entra em `main` sem pull request.** Todo trabalho acontece num branch, o CI roda em cada PR (e em `main` depois de cada merge, não no push de branch sem PR), e o merge só acontece depois de revisão.

**Nome de branch:** `<tipo>/<descrição>`, com o tipo da lista abaixo e a descrição em inglês, minúscula, palavras separadas por hífen, só `a-z`, `0-9` e `-`, até 50 caracteres no total. Número de issue, quando houver, abre a descrição: `fix/12-bgzf-bsize`. O branch diz o que muda; a sessão de trabalho fica registrada no PR.

**Mensagem de commit** (Conventional Commits):

```
<tipo>(<escopo>): <resumo>

<corpo: por quê, não o quê>

<trailers>
```

- Tipos: `feat`, `fix`, `test`, `docs`, `refactor`, `perf`, `ci`, `build`, `chore`, `style`, `revert`. Não existe `wip`.
- Escopo opcional: pacote ou área (`format`, `facts`, `verifier`, `cli`, `corpus`, `verify-cert`).
- Resumo em inglês, no imperativo, sem ponto final, até 72 caracteres; minúsculo, exceto sigla ou nome próprio.
- Corpo obrigatório em `feat` e `fix`: explica o porquê. O "o quê" está no diff.
- Mudança incompatível: `!` depois do tipo e uma linha `BREAKING CHANGE:` no corpo.
- Trailers: `Co-Authored-By:` quando um agente escreveu o commit; `Refs: #N` para ligar issue ou PR.
- O título do PR segue o mesmo formato. O commit de merge gerado pelo GitHub é a única exceção.

Commits pequenos, por fatia ou sub-fatia. `.github/check-names.sh` confere nome do branch, título do PR e cada commit do PR no CI do PR; branch ou mensagem fora do padrão deixa o PR vermelho. A regra vale do PR que a introduziu em diante; o histórico anterior não é reescrito por ela.

**`main` é protegido no GitHub** pela regra versionada em `.github/rulesets/main.json`: sem push direto, sem `push --force`, sem apagar o branch, e merge só por PR com o check `test` (o job do CI, publicado pelo GitHub Actions) verde e atualizado com `main`. Só merge commit, para preservar os commits pequenos. Ninguém tem bypass, nem o dono da org. O plano gratuito só aplica a regra em repositório público; ela entra no dia em que o repositório fica público:

```
gh api -X POST repos/colligo-go/colligo/rulesets --input .github/rulesets/main.json
```

Depois de ativa, o histórico de `main` nunca mais é reescrito. Qualquer limpeza de histórico acontece antes. O GitHub não oferece no plano gratuito regra de nome de branch ou de mensagem de commit; essas continuam conferidas pelo CI.

**Repositório público desde a Fatia 0.**
