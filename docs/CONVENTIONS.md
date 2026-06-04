# Colligo — Convenções

Estilo da casa. Inviolável durante implementação. Curto de propósito.

---

## Código

**Sem comentários. Nenhum.** Nem em Go, nem em SQL, nem em TOML de exemplo. Nomes de funções, variáveis e tipos comunicam intenção. Se um trecho precisa de comentário para ser entendido, ele precisa ser reescrito, não comentado.

**Erros são valores e são tratados.** Nada de `_` engolindo erro fora de casos genuinamente irrelevantes. Erros de domínio são tipados (ver SPEC §9) e inspecionáveis via `errors.As`.

**Concorrência via `errgroup` com `SetLimit`.** Nunca `go func` solto. Um erro cancela os irmãos via context; espera-se todos. Todo código concorrente é testado com `-race`.

**`context.Context` é o primeiro parâmetro** de toda operação que faz I/O ou pode ser cancelada. Cancelamento salva progresso parcial antes de encerrar.

**Functional options** para construção pública (`Open`, `New`). Uso mínimo tem defaults sensatos; controle fino é opcional.

**Conteúdo de arquivo nunca em memória.** Streaming para disco, sempre. `io.CopyBuffer` com buffer de 1MB.

**Stdlib primeiro.** Dependência externa só onde a stdlib é genuinamente insuficiente. As externas conscientes da v1: `modernc.org/sqlite`, um pacote BLAKE3, `golang.org/x/sync/errgroup`, `golang.org/x/time/rate`, e um cliente FTP isolado. Qualquer adição além dessas precisa de justificativa.

---

## Banco

**Transições de estado nunca sobrescrevem.** São linhas novas em `file_events`. O banco é o estado canônico; TSV é exportação, não fonte.

**Todas as escritas passam pelo writer goroutine.** Workers não escrevem direto no SQLite.

**PRAGMAs sempre.** Nenhuma conexão abre sem `foreign_keys=ON`, `journal_mode=WAL`, `synchronous=NORMAL`, `busy_timeout=5000`.

---

## Testes

**Offline e determinístico.** Servidor HTTP fake com fixtures reais commitadas. Nenhum teste toca a rede de verdade.

**Injeção de falha explícita.** Os caminhos de erro (Range ignorado, corpo truncado, reset, 429) têm teste próprio — é onde a robustez se prova.

**Teste escrito junto com o código**, não depois. Exemplos executáveis (`ExampleXxx`) onde fizer sentido como documentação viva.

---

## Documentação

**GoDoc em tudo que é exportado.** Função, método, struct. Escrito junto com o código. (GoDoc é documentação de API, não comentário de implementação — esta é a única forma de texto explicativo permitida no código.)

**Decisão muda código e doc no mesmo commit.** SPEC e implementação não desincronizam.

---

## Git

**`CLAUDE.md` é privado** (`.gitignore`). Não vai para o repositório.

**Commits pequenos, por fatia ou sub-fatia.** Mensagem descreve o que mudou e por quê em uma linha.
