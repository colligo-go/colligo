# Contribuindo com o Colligo

---

## O contrato

Para adicionar uma fonte: implemente `Source`, grave fixtures sintéticos, e chame `sourcetest.Run`.

Se passar, aceitamos sem auditar sua lógica de parsing linha por linha. **O que garante o usuário não é nossa revisão — é a suíte.** Se você encontrar um jeito de passar na suíte e ainda assim entregar dado errado, isso é um bug na suíte e queremos saber.

Adapters são puros: não recebem `Store`, não recebem caminho de disco, não abrem arquivo. O pior bug possível num adapter é devolver metadado errado, que o validador pega. Um adapter não pode corromper o acervo, por construção.

---

## Fluxo de um PR

1. Branch a partir de `main` atualizado, com nome `<tipo>/<descrição>`: `feat/ena-source`, `fix/12-bgzf-bsize`.
2. Commits em Conventional Commits: `fix(format): reject a leading UTF-8 BOM`, com corpo explicando o porquê em `feat` e `fix`.
3. PR com título no mesmo formato. O CI confere nome do branch, título e commits, e roda a suíte inteira.
4. Merge só com o CI verde e depois de revisão. Nada entra em `main` de outro jeito.

As regras completas estão em `docs/CONVENTIONS.md`, seção Git.

## Antes de abrir PR

```
gofmt -l .
go vet ./...
go build -o /tmp/noleak ./internal/noleak/cmd/noleak && go vet -vettool=/tmp/noleak ./...
COLLIGO_PYTHON=<python com blake3> COLLIGO_REQUIRE_PYTHON=1 go test -race -count=2 ./...
go test -run '^$' -fuzz=Fuzz -fuzztime=60s ./source/<sua-fonte>
```

`gofmt -l .` não pode imprimir nada. O verificador independente precisa de um Python com o pacote `blake3`; sem `COLLIGO_REQUIRE_PYTHON=1` o teste dele é pulado, e o CI não aceita isso.

Decisão muda código e documento no mesmo commit.

Sem comentários no código. GoDoc só em identificador exportado, e só quando diz o que o nome e a assinatura não dizem. Detalhe em `docs/CONVENTIONS.md`.

---

## Adapter para fonte privada

Fonte institucional funciona. A arquitetura não distingue origem: se você consegue descrever onde os arquivos estão e o que se sabe sobre eles, cabe num adapter.

### Primeiro: confirme que você precisa de um

Boa parte do dado institucional não chega por API. Chega como diretório montado, ou export com uma planilha descrevendo os arquivos. Nesse caso **não escreva adapter**: use `colligo adopt --manifest lista.csv`. Nada específico da instituição passa a existir em código nenhum, e você ganha custódia completa com reverificação eterna.

Escreva adapter só se a fonte realmente exigir chamada programática.

### A separação obrigatória

Três coisas que precisam ficar separadas:

| O quê         | Onde vive        | Publicável |
| ------------- | ---------------- | ---------- |
| Código        | repositório      | sim        |
| Configuração  | TOML local, env  | não        |
| Fixtures reais| `testdata/real/` | não        |

**Se é específico da instituição, é configuração, não código.** Aplicada com disciplina, sobra um adapter que descreve um *formato*, não uma instituição — e isso é publicável sem constrangimento.

Vaza pelo código, mesmo sem fixture:

- Nome de host interno escrito direto no código
- Nome de campo que revela o que a instituição coleta (`prontuario`, `data_nascimento`)
- Identificador de projeto ou de estudo controlado
- Credencial esquecida

Credencial vazada não se desvaza: o histórico do Git é para sempre. `gitleaks` roda em pre-commit por isso.

### Onde o adapter privado mora

Não precisa estar dentro do repositório do Colligo. É um módulo separado que importa o Colligo — pode viver em repositório interno ou só numa pasta na sua máquina.

Isso funciona porque fontes são registradas explicitamente em `colligo.WithSources(...)`, não por descoberta automática. O core não precisa saber que seu adapter existe.

### Teste de adapter privado

Adapter privado não tem revisão da comunidade. Ninguém além de você lê aquele código. **A suíte de conformidade deixa de ser conveniência e passa a ser o único revisor que existe.** Roda `sourcetest` igual aos públicos, sem exceção.

Fixture sintética resolve o teste público: um registro falso com a mesma forma do real, valores inventados. Entenda a limitação — fixture sintética testa se o parser está certo, **não** se a realidade continua igual. Se o servidor passar a devolver um campo novo, seu teste segue verde e você quebra na prática.

Por isso o trabalho é duplo:

- **Público:** conformidade contra fixture sintética. Roda para qualquer um, sempre.
- **Local:** teste contra fixtures reais, atrás da tag `restricted`, mais o job de drift semanal que regrava contra a API real. Nunca sai da sua máquina ou da infra da instituição.

O segundo é onde a detecção de mudança realmente mora.

### Classificação

Fonte restrita declara `classification = "restricted"` na configuração. Isso liga todo o regime de `CONFIDENTIALITY.md`: corpo de resposta não persistido, nomes opacos no export, permissão de banco enforçada, `debug` proibido.

A declaração é obrigatória e não tem default. Uma fonte sem classificação causa erro de carregamento.

### Um aviso fora do técnico

Publicar o adapter pode ser restrito pelo acordo de uso do dado, não só pelo bom senso. Alguns termos proíbem descrever a estrutura interna ou o endpoint, mesmo sem dado nenhum.

Antes de subir um adapter que descreve API institucional, pergunte a quem cuida de governança de dados. São cinco minutos de conversa contra um problema que não tem desfazer.

---

## Adicionando um validador de formato

Implemente `FormatValidator` com prefixo mágico e verificação de término. Não faça parsing completo — a pergunta é "o arquivo termina onde deveria terminar?", não "o conteúdo faz sentido?".

Todo validador precisa de teste com: arquivo válido, arquivo truncado em 10 offsets aleatórios, arquivo com 1 byte corrompido no meio, arquivo vazio, e página HTML de erro renomeada com a extensão do formato.

---

## Reportando bug

Use `colligo redact-report` e cole a saída. Ela é feita para ser pública: contém versões, hash de config, contagem de estados e tipos de erro, e nenhum identificador.

Não cole log cru, não cole stack trace, não cole caminho de arquivo. Se o relatório redigido não é suficiente para diagnosticar, isso é um bug no relatório e queremos saber.
