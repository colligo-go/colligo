# Colligo — Design e Decisões

O propósito deste documento é registrar **por que** o Colligo é como é. Como implementar está em `SPEC.md`. Defesas contra erro silencioso, em `RELIABILITY.md`. Proteção de dado restrito, em `CONFIDENTIALITY.md`. O que vem depois, em `ROADMAP.md` e `BACKLOG.md`. O que já existe e onde o Colligo se encaixa, em `RELATED.md`.

---

## 1. Visão

Colligo é uma biblioteca Go e um CLI que respondem uma pergunta sobre dados genômicos:

> Eu tenho os dados que acho que tenho?

O nome vem do latim _colligere_ — "eu coleto, eu reúno, eu agrego".

A distinção importa e define o projeto. Baixar dado público é problema resolvido: iSeq, fetchngs, ffq, pysradb, fastq-dl e outros fazem isso bem, com MD5 e resume. Garantir que os **bytes** continuam os mesmos ao longo dos anos também é resolvido, fora da bioinformática: git-annex (`fsck --incremental-schedule`), DataLad e DVC fazem isso. Ver `RELATED.md`.

O que nenhuma dessas ferramentas sabe é o que o arquivo **significa**. Que o `.fastq.gz` terminou onde deveria. Que tem o número de reads e de bases que a fonte publicou. Que R1 e R2 são um par. Que o GTF é GENCODE 44 com `chr1`, e não Ensembl 111 com `1`. Que 47 de 50 não é completo. Que a fonte suprimiu o run como incorreto depois que você o baixou.

Colligo faz três coisas, sempre sobre dado com semântica de bioinformática:

- **Identificar:** que arquivo é este de fato — release, assembly, coleção de sequências — deduzido do conteúdo, não do nome.
- **Verificar:** está inteiro e é o que a fonte e você declararam.
- **Vigiar:** continua valendo, no disco e na fonte.

Em uma frase: Colligo não prova que você baixou os bytes certos — isso todo mundo faz. Prova que você tem os dados que acha que tem, e avisa quando isso deixa de ser verdade.

**Faz:** extrai fatos do conteúdo numa única leitura, compara o declarado contra o observado, busca metadados em fontes públicas e privadas, organiza o metadado em estrutura experimental tipada, baixa com integridade e resume, **adota** arquivo que chegou por fora, persiste tudo em SQLite com histórico auditável, exporta manifesto portátil, atesta acervo contra manifesto de terceiro e reconsulta a fonte para detectar mudança.

**Não faz:** análise, gerenciamento de pipeline, visualização, criptografia em repouso, anonimização de conteúdo.

### Duas portas, mesma biblioteca

**Porta leve — `colligo check`.** Um comando, sem banco, sem TOML, sem estado salvo. Lê cada arquivo uma vez, extrai os fatos e, com `--source`, compara contra o que a fonte declarou. Código de saída diferente de zero se algo não bate; `--json` para máquina. Distribuída via bioconda e como módulo nf-core. É a porta que gera usuários: não exige ler documentação nenhuma.

```
colligo check SRR2133898_1.fastq.gz SRR2133898_2.fastq.gz --source ena
```

**Porta pesada — `fetch`, `adopt`, `status`, `manifest`, `attest`, `recheck`.** Banco SQLite, execuções registradas, vigilância no tempo, manifesto, confidencialidade. Para quem precisa de custódia: core facilities, reanálise em escala, consórcios, dado controlado.

**Regra:** a porta leve não depende de nada da pesada. Extratores de fatos, validadores de formato e a parte de metadados dos adapters vivem no núcleo e servem às duas. A pesada só acrescenta persistência e tempo.

### Quem usa

Quem reanalisa dado público em escala (meta-análise, atlas, treino de modelo), quem desenvolve e compara métodos, grupos clínicos com dado controlado, core facilities, consórcios que trocam acervos, e quem precisa reproduzir a própria tese anos depois. O aluno que baixa seis amostras uma vez usa a porta leve e não precisa saber que a pesada existe.

---

## 2. Princípios

**Declarado contra observado, em todas as camadas.** Toda checagem do Colligo compara o que alguém afirmou com o que o conteúdo mostra. A fonte declara MD5, `read_count`, `base_count` e o conjunto de arquivos; você declara tipo, release e assembly; o nome do arquivo declara o par. O arquivo é a testemunha. Nenhuma ferramenta cruza as três partes — fonte, declaração e conteúdo — e acompanha isso no tempo. Esse é o diferencial.

**O usuário é a fonte de verdade sobre intenção.** Ele declara o tipo de dado antes de começar. A API confirma, não descobre. O conteúdo confirma ou desmente.

**Falhar fechado.** "Nunca silencioso" implementado como aviso produz avisos que ninguém lê. Discrepância que compromete o resultado **para** o pipeline. Modo estrito é default; `--permissive` muda se para, nunca se a informação existe.

**Dado parcial tem valor.** 50GB baixados até 80% são horas de transferência. Nunca se apaga progresso parcial. Isto inclui o caminho de `ENOSPC`: grava-se o offset seguro antes de propagar o erro.

**Auditabilidade total.** Toda transição de estado e toda chamada de API ficam registradas com timestamp e vinculadas à execução que as causou.

**Proteção por tipo, não por disciplina.** Dado restrito não vaza porque o caminho errado não compila, não porque alguém lembrou de redigir.

**Extensibilidade por design, com adapters impotentes.** O core é agnóstico ao tipo de experimento. Adapters são código puro sem acesso a disco ou banco — o pior bug possível num adapter é devolver metadado errado.

---

## 3. Decisões cravadas e seus porquês

### Adapters são puros

A interface original dava `Download` ao adapter, o que significa que o código do colaborador escreve no disco e no banco. Um bug oculto dele pode gravar path absoluto, pular transição de estado, ou escrever fora do diretório do estudo. Nenhum teste de contrato conserta isso, porque o adapter tem acesso direto às primitivas.

Invertido: adapter recebe accession, devolve dados. O core é o único que baixa, hasheia, transiciona e escreve. **O pior bug possível passa a ser "retornou metadado errado"**, que o validador pega e `discrepancies` registra. Isolamento por construção, não por disciplina.

Custo: protocolo exótico precisa de uma segunda interface, `Transport`, de superfície mínima, revisada com rigor de core.

Consequência para governança: `source/*` pode ser mergeado por qualquer mantenedor, precisamente porque a suíte de conformidade é o gate.

### `adopt` é tão importante quanto `fetch`

Dado controlado — dbGaP, institucional — não chega por HTTP com `Range`. Chega por `prefetch`, Globus, ou disco físico. Toda a máquina de faixa/retry/resume não se aplica.

Isso parecia limitação e é o contrário: revela que o produto não é `download`, é **custódia**. `adopt` registra um arquivo que chegou por qualquer meio, constrói a árvore e extrai os fatos, e dali em diante ele tem reverificação eterna igual aos baixados. Adoção é registro, não transferência — o arquivo não é copiado nem movido.

Consequência honesta: arquivo adotado sem checksum publicado tem integridade garantida **a partir do momento da adoção**. Colligo não pode afirmar que ele chegou correto. A distinção é registrada no estado `adopted` em vez de dissolvida em `done`.

### Fatos e afirmações

A decisão estrutural central. Tudo que o Colligo sabe sobre um arquivo se divide em duas classes.

**Fatos** são extraídos do conteúdo numa única leitura em streaming: raiz BLAKE3, MD5 e SHA-256, validade estrutural, contagem de registros e de bases, primeiro e último ID de read, nomes de sequência amostrados, digest de Sequence Collection. Um fato é função pura do conteúdo e da versão do extrator. Por isso é indexado por `(raiz, tipo, versão do extrator)` e nunca recalculado enquanto a raiz não mudar. Dois arquivos com o mesmo conteúdo compartilham fatos de graça.

**Afirmações** são comparações baratas entre fatos, declarações e o que a fonte publicou: o MD5 bate, a contagem de bases bate, R1 e R2 casam, o release confere, o conjunto está completo. O estado de custódia é função das afirmações, não uma coluna que alguém atualiza.

Consequências:

- **Hash igual implica estrutura igual.** Se a raiz confere, a validação estrutural não pode ter mudado de resultado. Revalidação só roda quando a versão do validador muda. Isso importa porque, para gzip simples e FASTQ, verificar o término exige descomprimir tudo.
- **A leitura cara paga por tudo.** Se o FASTQ precisa ser descomprimido para validar estrutura, a mesma passada conta reads e bases, registra os IDs das pontas e alimenta as invariantes de par e de fonte. Ler o arquivo duas vezes é bug de desenho.
- **A porta leve é a extração de fatos sem persistência.** A pesada é a mesma extração com banco.

### A árvore BLAKE3 unifica resume, verificação e atestação

BLAKE3 já é uma árvore de Merkle. Em vez de uma grade própria de hashes independentes — que seria uma segunda estrutura podendo discordar do hash global —, o Colligo guarda a codificação **outboard do Bao**: os nós internos da árvore, com grupo de 8 MiB, num arquivo à parte. A raiz dessa árvore **é** o BLAKE3 do arquivo. Uma estrutura, uma raiz, consistência por construção.

Resume, localização de dano, scrub e atestação viram a mesma operação: verificar uma fatia contra a raiz. O manifesto carrega raiz e outboard; quem recebe verifica fatias sem confiar em ninguém.

Limitação aceita: corrupção que desloca bytes faz todas as fatias seguintes falharem, então o reparo rebaixa do ponto do erro até o fim. Continua correto, só perde a economia nesse caso raro.

Limitação honesta: localizar dano no nível do bloco só economiza algo quando existe reparo. Enquanto reparo granular estiver no backlog, a granularidade serve ao resume e ao diagnóstico, não à economia. A árvore não é o diferencial do projeto; é infraestrutura barata que o BLAKE3 já dá.

### Integridade interna é BLAKE3; MD5/SHA da fonte é juiz único

BLAKE3 paraleliza um arquivo em vários núcleos e passa de vários GB/s. MD5 fica em ~600 MB/s num núcleo. Integridade contínua usa a árvore BLAKE3; o MD5/SHA da fonte é cross-check do arquivo inteiro, calculado de graça na mesma leitura.

### Comparar `stat` não é verificar

Tamanho e mtime iguais ao baseline dizem que ninguém mexeu no arquivo pelas vias normais. Não dizem nada sobre corrupção silenciosa do disco, que não altera nenhum dos dois. O gate de `stat` existe para decidir o que precisa ser lido, nunca para declarar um arquivo verificado. Só leitura de conteúdo atualiza `last_verified_at`.

### Integridade de bytes não basta

Checksum correto não significa arquivo correto. Um GTF do Ensembl 110 e do 111 são ambos íntegros e produzem resultados diferentes. Um `.gz` truncado descomprime parcialmente e muita ferramenta downstream aceita sem reclamar.

Por isso há quatro camadas além do hash: **identidade** (assembly, release, ETag, digest de coleção de sequências), **estrutura** (bytes mágicos e verificação de término por formato), **invariante de par** (contagem e IDs de R1/R2) e **invariante de fonte** (contagem de reads e bases contra o que a fonte publicou). O catálogo completo está em `RELIABILITY.md`, que é o documento que responde ao medo que originou o projeto.

### Identidade deduzida do conteúdo

Declarar `release = "GENCODE 44"` no TOML é necessário, mas é declaração. O Colligo confirma pelo conteúdo quando pode:

- **FASTA:** digest de Sequence Collection (refget, GA4GH). Independe de compressão, largura de linha e texto de cabeçalho. Identifica a coleção pelo que ela contém.
- **GTF do GENCODE:** a versão está na linha `##description` do cabeçalho.
- **GTF do Ensembl:** o cabeçalho traz `#!genome-build` e datas, sem número de release. Aqui a identificação é por hash contra os arquivos de checksum oficiais de cada release.

Confirmado pelo conteúdo, `release_confidence` vira `confirmed`. Resolve também o arquivo órfão no servidor que ninguém sabe de qual release é: `colligo identify arquivo.gtf.gz`.

### Compatibilidade é relação entre conjuntos

`chr1` contra `1` é o caso visível de um problema geral: arquivos que precisam compartilhar um sistema de coordenadas. O padrão Sequence Collections define `sorted_name_length_pairs`, digest do sistema de coordenadas que ignora ordem, e uma função de comparação que devolve o que só existe em A, o que só existe em B, o que é comum, e se a ordem coincide.

`.fai`, `@SQ` do BAM e `##contig` do VCF viram pares nome-comprimento e são comparados por igualdade de digest. GTF e BED só têm nomes, então viram teste de inclusão. Compatibilidade deixa de ser heurística e vira relação bem definida. Implementado em `compat/`, sobre os fatos já extraídos.

### A fonte também muda

Um run do ENA suprimido some da busca, mas continua recuperável por accession. Supressão permanente é para dado considerado incorreto sem perspectiva de correção. Um pipeline pode continuar usando, sem aviso, um dado que a própria fonte condenou.

`colligo recheck --upstream` reexecuta só a parte de metadados dos adapters — barata, sem baixar nada, pura — e compara com o que foi guardado: status do registro, conjunto de arquivos, checksums publicados. Divergência vira `discrepancy`. É a vigilância do lado da fonte, complemento do scrub do lado do disco.

### Validação estrutural antes do rename

O `.part` só vira nome final depois de checksum **e** validação estrutural. Isso significa que a existência do arquivo com nome final é ela mesma uma afirmação: "este arquivo passou tudo". Nenhum pipeline downstream consegue pegar um arquivo meio-validado por acidente, porque ele não existe com aquele nome.

### Falhar fechado, com códigos de saída distintos

`3` significa "terminou e está errado". `4` significa "não terminou". Um script que trata os dois igual perde a diferença que importa — e a diferença entre incompleto e interrompido é justamente a que decide se você reroda ou investiga.

### Classificação obrigatória e imutável

Fonte sem `classification` declarada causa erro de carregamento. Não há default: um default `public` vazaria dado restrito por omissão, e um default `restricted` treinaria o usuário a desligar a proteção.

Estudo restrito é restrito por inteiro, não por linha, porque correlação é informação: saber que dois pacientes estão no mesmo estudo já diz algo. E a classificação é imutável — promover estudo público a restrito seria gambiarra, porque os dados já foram gravados em claro. O erro instrui a criar estudo novo.

### Desconfiar da própria ferramenta

Se o cálculo de hash tem bug, tudo "verifica" e tudo está errado. Nenhuma defesa interna alcança isso, porque a defesa usa o código defeituoso.

Daí três coisas que parecem paranoia e não são: vetores conhecidos de hash rodados em toda abertura, um arquivo canário verificado pelo mesmo caminho de código dos arquivos reais, e saída compatível com `b3sum`/`sha256sum` para conferência por ferramenta externa. Duas implementações independentes da checagem crítica é a única defesa real contra bug na checagem.

O canário tem um segundo efeito: quando ele falha, a mensagem diz que o problema é ambiente ou ferramenta, para você não sair caçando corrupção onde não há.

### Linguagem: Go, com uma porta estreita para Rust

Go não é a linguagem mais rápida para o gargalo do projeto. Descompressão e parsing em Rust ou C, com libdeflate, zlib-ng e SIMD, vão mais longe; o ecossistema de bioinformática em Rust (noodles, needletail) também é mais completo, principalmente para CRAM.

Go foi escolhido pelo resto do sistema: rede, retry, rate limit e fan-out de metadados são naturais; binário estático com cross-compile trivial; inicialização instantânea, que importa ao rodar `check` em milhares de arquivos ou como passo curto de pipeline; empacotamento como biocontainer e módulo nf-core quase sem esforço. E velocidade de desenvolvimento, que num projeto de uma pessoa pesa mais que um fator constante de throughput.

A diferença real é menor do que parece. Em download a rede domina. Em arquivo local o piso é a descompressão, e o `zcat` é C; Go com `klauspost/compress` fica cerca de 20% atrás dele na medição da Fatia 1. É fator constante, não ordem de grandeza.

Linguagem não é diferencial. O usuário escolhe ferramenta pelo que ela pega e pelo atrito de usar; Go reduz o atrito, mas não é o motivo de alguém usar. E linguagem não protege ideia: a checagem de `read_count` cabe num script Python dentro de qualquer workflow. O que não se copia rápido é a arquitetura de fatos e o benchmark como prova.

**A porta para Rust.** Se o benchmark de `TESTING.md` §7.1 mostrar a descompressão em Go longe demais do piso, a saída não é reescrever o projeto. A fronteira já existe: o extrator de fatos é função pura de bytes para fatos, sem banco, rede ou config. Um acelerador Rust opcional entra ali, como **processo separado** — não cgo, não WASM:

- O produto é o Go, completo sozinho. O acelerador é usado se estiver no PATH; se não, o extrator Go roda.
- As duas implementações produzem fatos idênticos byte a byte, garantido por teste diferencial e fuzz entre elas.
- Uma fronteira só (o extrator), um sentido só (Go chama Rust), um contrato só (o esquema de fatos, com `extractor_version` compartilhada).

cgo com biblioteca Rust seria mais rápido na fronteira, mas exige toolchain Rust e C no build, complica o cross-compile e mata o binário puro-Go. WASM via wazero evita cgo, mas perde a velocidade nativa, que era o único motivo. Processo separado custa milissegundos por arquivo para subir, irrelevante perto de descomprimir gigabytes.

O efeito colateral é o melhor argumento: duas implementações independentes do extrator são exatamente a defesa de M9 contra bug na própria ferramenta. O acelerador Rust é velocidade e segunda opinião na mesma peça.

Decisão adiada com gatilho em `BACKLOG.md`. Até lá, a regra que vale agora é a de fronteira: nada fora do extrator pode depender de detalhe de implementação do extrator.

### Driver SQLite: `modernc.org/sqlite` (Go puro, sem CGO)

Multiplataforma com binário simples é incompatível com CGO na prática. O `modernc` é mais lento, mas o gargalo é sempre rede e disco dos arquivos, nunca o banco de metadados. Pagamos moeda que sobra para comprar moeda que falta.

Consequência: a documentação **não** afirma "tudo stdlib". É "stdlib + um driver SQLite puro-Go".

### PRAGMAs lidos de volta

`foreign_keys=ON` porque o SQLite não enforça FK por padrão — sem isso a integridade referencial é decorativa. `journal_mode` resolvido pelo filesystem — `WAL` em disco local, `DELETE` em filesystem de rede, onde WAL é corrupção silenciosa (`CONTRACT.md` §2) —, `synchronous=NORMAL` (seguro sob WAL, muito mais rápido que FULL), `busy_timeout=5000`, transações de escrita com `BEGIN IMMEDIATE`.

Versão anterior forçava WAL sempre. Com o Scribo detectando filesystem de rede e o Colligo não, um banco compartilhado em NFS seria protegido por uma ferramenta e corrompido pela outra. A regra agora é única e vive no contrato.

Novidade: os valores são **lidos de volta** após o SET. Um PRAGMA que não surtiu efeito é exatamente o tipo de falha silenciosa que este projeto persegue.

Armadilha real, já cometida uma vez neste código: `*sql.DB` é um pool. `foreign_keys` e `busy_timeout` valem por conexão, então um `db.Exec("PRAGMA …")` configura só a conexão que o pool entregou naquele momento. Os PRAGMAs vão no DSN (`_pragma=` do `modernc.org/sqlite`), que os aplica a toda conexão nova, e a leitura de volta roda em cada conexão aberta.

### Confidencialidade entra pelos tipos, não pelo regime inteiro

O regime de `CONFIDENTIALITY.md` é o documento mais pesado do projeto, e a primeira fonte restrita só aparece na Fatia 6. Construir o regime inteiro antes de o Colligo baixar um arquivo seria a Fatia 0 sem entregar nada utilizável.

O que não dá para encaixar depois entra primeiro: os tipos `secret.Secret`, `secret.Name` e `secret.Endpoint`, e o `logf` tipado. Se o código nasce passando `string`, retrofitar é reescrever. Todo o resto — permissões, pasta sincronizada, sentinela global, dialer instrumentado, `doctor`, `redact-report` — se adiciona depois sem dor e entra junto com a primeira fonte restrita.

### Writer goroutine único, com erro e flush

Um goroutine serializa todas as escritas, eliminando contenção. Não é fire-and-forget: cada evento carrega canal de resposta e o worker espera o ack — senão disco cheio passa despercebido. Transições `fatal` são commitadas sem batch. No shutdown: cancela, drena, commita, fsync, grava o desfecho da execução, sai.

### Toda execução é registrada

`tool_runs` grava versão, git SHA, hash de config e código de saída. Todo evento de arquivo aponta para a execução que o causou.

Sem isso a auditoria diz "o que aconteceu" mas não "com qual código" — e "com qual código" é metade da reprodutibilidade. Também é o que permite detectar execução interrompida e esquecida, e configuração que mudou no meio de um acervo.

### `reference` em vez de `genome`

O tipo `genome` original pretendia gerenciar assets de referência, incluindo índices de aligner. Isso compete de frente com o refgenie, que faz melhor.

Reduzido a `reference`: baixa FASTA, GTF/GFF3, transcritos e blacklist, e registra **qual release exatamente**, com a convenção de cromossomo detectada. Nada de construir índices. É 10% do refgenie e é justamente a parte que decide reprodutibilidade.

### Convenção de cromossomo nunca é convertida em silêncio

`chr1` do GENCODE contra `1` do Ensembl. Se genoma e anotação divergem, a quantificação falha sem erro nenhum — só números errados.

`chromosome_naming` é metadado de primeira classe, detectado por amostragem. Derivado cujos pais divergem é `fatal`. Colligo avisa e para; converter automaticamente seria criar o erro silencioso que se tenta evitar.

### Adoção pelo canal que o usuário já usa

A ferramenta que a comunidade adota funciona com um comando e aparece onde o usuário já olha. Por isso três decisões de distribuição são de primeira classe, não polimento:

- **`check --samplesheet`** confere a samplesheet de um pipeline nf-core pelo conteúdo — arquivos, par, contagens, repetição. O nf-schema valida as colunas da planilha, não o que os arquivos contêm.
- **Módulo MultiQC** mostra o resultado do `check` no relatório que todo mundo já abre. Ninguém precisa mudar de hábito para ver.
- **`colligo.lock`**, o `go.sum` dos dados: pequeno, textual, versionado junto da análise, conferido no início do pipeline. A ideia de lockfile não é nova — o DVC tem seus arquivos `.dvc` —; o conteúdo é: identidades confirmadas e contagens, não só hashes.

### Declarado contra observado, além do arquivo isolado

O mesmo princípio vale para metadado de estudo:

- **Química de single-cell:** comprimento do R1 e barcodes contra whitelists denunciam a química; divergência da declarada para antes do alinhamento.
- **Matriz suplementar do GEO:** colunas contra a lista de GSMs, namespace de ID de gene contra o genoma declarado.
- **Duplicata entre accessions:** conteúdo idêntico aparece de graça porque fatos são indexados pela raiz.

### Adorável, visivelmente rápido, demonstravelmente confiável

Correto não basta. Uma ferramenta que é só mais correta que as outras é respeitada e pouco usada. Três eixos, cada um com decisões concretas.

**Adorável — resolver algo chato em um comando e explicar o que achou.**

- **`colligo get GSE116256`**: da accession à samplesheet validada. Busca metadados, baixa, verifica na mesma leitura, organiza as amostras por papel e réplica, e entrega `samplesheet.csv` pronta para nf-core mais `colligo.lock`. É porta leve: sem banco; o estado de resume vive ao lado dos arquivos (`.part` e outboard). O fetchngs cobre parte disso, mas exige Nextflow; aqui é um binário.
- **Mensagem de erro é diagnóstico.** Toda afirmação que falha aponta uma entrada de um catálogo de diagnósticos: o que foi observado, a causa provável, o que fazer. "R2 tem 1,2 milhão de reads a menos que o ENA declara. Padrão típico de extração truncada. Refaça com `fasterq-dump` ou baixe o FASTQ do ENA." Código de saída continua existindo para máquina; a pessoa lê a frase.
- **Zero configuração.** `colligo check .` descobre formatos pelo conteúdo e pares pelo nome (`_R1`/`_R2`, `_1`/`_2`), e roda. TOML só quando o usuário quer controle.

**Visivelmente rápido — paralelismo que outros não têm.**

- **Verificação sobreposta ao download.** As outras ferramentas baixam e depois releem o arquivo para conferir o MD5. No Colligo os fatos são calculados enquanto os bytes chegam; quando o download termina, a verificação terminou. Meta mensurável: `get` com verificação completa custa no máximo 5% acima do download puro.
- **Concorrência adaptativa por servidor.** Número fixo de conexões ou subutiliza o servidor ou leva bloqueio. O Colligo ajusta sozinho, como o controle de congestionamento do TCP: aumento aditivo enquanto a vazão cresce, redução multiplicativa ao primeiro 429, 503, timeout ou queda de vazão. O limite configurado por domínio vira teto, não valor. O usuário não precisa adivinhar `--workers`.

**Demonstravelmente confiável — provar que a própria ferramenta não mente.**

- **Crash em qualquer ponto.** A suíte mata o processo em cada escrita, `fsync` e `rename` do resume e do banco, e verifica a invariante depois de cada retomada: nenhum arquivo com nome final sem ter passado em tudo, nenhum progresso perdido além do último grupo. Ferramentas sérias de sistema de arquivos fazem isso; de bioinformática, quase nunca.
- **Modelo formal do resume e da máquina de estados**, em TLA+, verificado por model checking: nenhuma sequência de falhas, reinícios e respostas do servidor leva a `done` sem conteúdo conferido. O modelo é escrito antes do código da Fatia 3 e fica no repositório.
- **Certificado verificável por terceiros.** `check --certificate` emite um JSON com raiz, tamanho, fatos, versões e afirmações. Um verificador de referência de ~200 linhas em Python, independente do código Go, confere o certificado contra o arquivo sem instalar o Colligo. Confiabilidade passa a ser algo que outra pessoa checa, não algo em que ela precisa acreditar — e o verificador é, de brinde, mais uma implementação independente para M9.

### Velocidade tem piso físico, e a meta é encostar nele

Validar gzip simples e FASTQ exige descomprimir tudo. O piso é a descompressão em Go puro; a meta é ficar a 10% dela, e na primeira medição o `check` inteiro ficou a 0,5%. O `zcat`, em C, é ~20% mais rápido que o inflate em Go: essa distância é reportada, não perseguida, porque fechá-la pede cgo ou descompressão especulativa. Três decisões seguem daí: a leitura única distribui o buffer em paralelo, uma goroutine por consumidor, porque `io.MultiWriter` somaria os custos; o parser opera sobre o buffer sem alocar; e o jeito mais rápido de verificar é não verificar de novo — fatos por raiz transformam a reexecução num `stat`. Modo rápido existe (`check --quick`), mas nunca declara nada verificado.

### Artefato derivado tem proveniência, mínima

Um GTF fundido de GENCODE com Dfam não foi baixado, foi construído. Sem saber qual versão de cada pai entrou, o resultado não é reproduzível, e a ausência é invisível.

`derived_files` e `derived_parents` registram pais, construtor, versão e hash de parâmetros. Derivado herda a classificação mais estrita dos pais — um derivado de pai restrito é restrito.

Escopo deliberadamente mínimo. Dentro de um workflow, proveniência de derivado é trabalho do orquestrador: o data lineage do Nextflow, experimental desde a 25.04, já registra caminho, tamanho e checksum dos outputs. O Colligo cobre o derivado construído fora de orquestrador e, quando fizer sentido, lê o registro do Nextflow em vez de competir com ele.

### Manifesto portátil

O banco é o estado canônico, mas um terceiro que recebe seu acervo não pode verificar nada sem ele. O manifesto exporta raiz e outboard, identidades e estado de custódia num JSON que circula.

Em estudo restrito o manifesto sai com nomes opacos e continua **completamente funcional** para atestação, porque hashes não são sensíveis. É o mecanismo que separa "reprodutibilidade interna" de "reprodutibilidade verificável".

### FTP isolado, HTTPS prioritário, Aspera fora da v1

Preferimos HTTPS e herdamos `Range`, retry e checksum do caminho principal. FTP fica isolado, com resume próprio, só onde não há alternativa. Aspera é binário externo com licença e configuração de rede fora do nosso controle: fora da v1.

### Concorrência idiomática e educada

`errgroup` com `SetLimit`: um erro cancela os irmãos e espera-se todos. Limites por domínio existem para não queimar o IP da instituição; aumentá-los é responsabilidade do usuário e fica registrado no hash de config.

### Infraestrutura

Go estável mais recente (habilita `iter.Seq`, que resolve iteração sobre estudos grandes sem carregar tudo em memória). Licença MIT. Módulo `github.com/<org>/colligo`.

Plataforma: o alvo de uso é Linux (HPC, servidores, WSL). O desenvolvimento acontece também em Windows, e isso tem consequência: permissões `0600`/`0700`, `verified_inode` e detecção de pasta sincronizada assumem POSIX. Decisão: desenvolver e testar no WSL; o binário nativo de Windows compila, mas só a porta leve é suportada nele até existir CI Windows. OneDrive é a pasta sincronizada padrão do Windows — exatamente o caso que `CONFIDENTIALITY.md` §5.3 quer pegar —, então estudo restrito em Windows nativo é recusado.

---

### Decisões da Fatia 1, com confiabilidade primeiro

- **Linha em branco no fim do FASTQ é `advisory`, não `blocking`.** É inofensiva e comum; bloqueá-la gera alarme falso, e alarme falso empurra o usuário para `--permissive`, que é onde problema real passa. Ela é contada no fato e relatada. Linha em branco *no meio* continua `blocking`.
- **Linha `;` no FASTA é rejeitada.** Antes ela era contada como bases: contagem errada em silêncio. Ignorá-la poderia esconder lixo. Num formato arcaico, falhar fechado custa pouco.
- **BSIZE falso em BGZF é rejeitado.** O subcampo `BC` promete BGZF, e htslib, samtools e tabix dependem do BSIZE para achar os blocos: com ele errado o gzip interno descomprime inteiro e o arquivo ainda assim falha, ou lê lixo, no acesso aleatório. Validar só o bloco EOF deixava passar um arquivo que o resto do ecossistema não lê. A cadeia de BSIZE precisa ser contígua do offset 0 até o fim exato do arquivo, e o verificador Python a confere de forma independente.
- **BOM UTF-8 no início é rejeitado em FASTQ, FASTA e GTF.** No GTF ele contaminava o primeiro seqname: `\ufeffchr1` ≠ `chr1`, e a convenção de cromossomo caía para `unknown` em vez de `ucsc`. É o erro silencioso da M11, sem aviso e com números errados adiante. Remover o BOM em silêncio seria converter sem avisar; o sniffer o ignora só para que o erro seja específico (`structure_invalid`, offset 0) e não um genérico "formato desconhecido".
- **O verificador independente distingue "discordo" (1) de "não sei julgar" (2).** Schema desconhecido é "não sei"; tratá-lo como discordância ou concordância seria afirmar algo que não foi verificado.
- **`fatal` de arquivo não interrompe os outros arquivos; `fatal` da ferramenta interrompe tudo.** Na porta leve não há estado a salvar, e parar no primeiro arquivo ruim esconderia o diagnóstico dos demais. O código de saída continua sendo 2.
- **Saída em inglês.** Uma língua só na interface pública; os identificadores no `--json` são estáveis independentemente do texto.
- **A meta de desempenho mede o que é nosso.** Ver "Velocidade tem piso físico". Trocar confiabilidade por velocidade — cgo, inflate especulativo — fica para depois e só com medição.
- **Corpus sintético versionado, com verdade do gerador.** Bytes fixos tornam certificados reprodutíveis por terceiros; o esperado vem de quem escreveu o arquivo, não de outro parser, o que pega o erro que o Go e o verificador Python compartilhariam.

## 4. Tipos de dado

Perfis plugáveis sobre um core agnóstico, mais `generic` como fallback sem schema.

| Tag          | Tipo                          | Fontes principais           | Controle          |
| ------------ | ----------------------------- | --------------------------- | ----------------- |
| `rnaseq`     | RNA-seq bulk                  | GEO, ENA, dbGaP             | Não               |
| `singlecell` | scRNA-seq (aquisição)         | GEO, ENA                    | Não               |
| `tabular`    | MAF, VCF, dose-resposta, clínico, matriz de assinatura | cBioPortal, portais, adoção | Não |
| `reference`  | FASTA, GTF/GFF3, transcritos  | GENCODE, Ensembl, Dfam      | N/A               |
| `structure`  | PDB, mmCIF, FASTA de proteína | RCSB PDB, adoção            | N/A               |
| `chipseq`    | ChIP-seq                      | ENCODE, ENA, GEO            | Sim — Input/IgG   |
| `generic`    | Desconhecido                  | Qualquer                    | Não               |

`atacseq`, `methylation` e `wgs` saíram da v1 para o `BACKLOG.md`. Não porque sejam difíceis — o padrão é o mesmo — mas porque nenhum projeto ativo os usa, e tipo construído especulativamente é tipo não testado contra realidade.

`singlecell` é **aquisição apenas**: valida metadados de biblioteca (10x Chromium, Drop-seq, Smart-seq2, química, layout). Validação de estrutura de output de single-cell fica fora do escopo — é do pipeline downstream. Robustez vem de fazer menos bem feito.

`tabular` é o tipo mais leve e o de proveniência mais crítica. Arquivos de KB a MB não precisam de outboard nem worker pool — canhão para mosca. Mas você precisa saber qual versão da matriz de assinatura usou e de que data é o export do portal. Caminho de arquivo pequeno: hash único, sem outboard, fatos e auditoria completos. É um modo, não um subsistema.

`role` (signal | control | reference) é mais fundamental que sample_type — toda estrutura experimental tem "o que meço" contra "meu controle".

Organizar o metadado é o segundo valor do projeto, depois da confiabilidade. Transformar um GSE numa planilha de amostras tipada e validada, com controle ligado ao sinal correto, é trabalho que muita gente faz à mão e erra. O fetchngs gera samplesheet para o nf-core; o que o Colligo acrescenta é validar que o controle declarado é de fato controle e que a estrutura experimental fecha.

---

## 5. Sistema de metadados em camadas

**Camada 1 — Core universal:** accession, source, role, layout, replicate, status, confidence, timestamps, classificação.

**Camada 2 — Perfis:** roles válidos, lógica de linking, campos obrigatórios, validações. Perfil customizado é registrável.

**Camada 3 — Declaração:** o TOML. O perfil expande a declaração mínima num contrato completo.

**Camada 4 — Confiança:** cada metadado carrega origem — `declared` (explícito da API), `inferred` (heurística, gera issue), `confirmed` (usuário validou). Exportado como `QC_Status`.

Validação em duas fases: local síncrona (campos, tipos, formato de accession — barata, para antes da rede) e remota assíncrona em fan-out (tipo declarado bate? accessions existem? conjunto de arquivos é o mesmo de antes?).

---

## 6. Relação com o Scribo

O Colligo garante as entradas antes do pipeline; o Scribo observa os resultados depois; cada lado corrige o outro. A integração é por contrato (`CONTRACT.md`, cópia idêntica nos dois repositórios): abertura do banco, `tool_runs`, e quem é dono de qual tabela.

**O que o Colligo dá ao Scribo:** identidade de amostra (`samples`), raízes verificadas (`files`), fatos para invariantes entrada × resultado (`facts`), status de accession na fonte, classificação do estudo. Com isso o Scribo liga cada valor às entradas de que depende, recusa resultado que contradiz a entrada — mais reads mapeados do que o FASTQ tem —, e lista os resultados obsoletos quando uma entrada é invalidada.

**O que o Scribo dá ao Colligo:**

- **Evidência depois do pipeline.** O Colligo não alinha reads, por decisão. Uma amostra com taxa de alinhamento muito fora das outras sugere organismo trocado ou arquivo errado; o Scribo detecta o desvio (z-score robusto) e grava sinal `suspect`. O Colligo o consome como afirmação a mais, sem cruzar a fronteira da análise.
- **Identidade confirmada por genótipo.** Saída do somalier ou do NGSCheckMate, comparada com o declarado, vira `identity_confirmed` ou `identity_mismatch` na custódia.
- **Plano de reprocessamento.** Quando `recheck --upstream` invalida uma entrada, o Scribo sabe quais resultados e etapas dependiam dela e gera a samplesheet só com as amostras afetadas.
- **Completude de ponta a ponta.** O `status` passa a mostrar o funil até "processado".
- **Prioridade de scrub e `gc` com proveniência**, a partir de `result_inputs`.
- **Corpus de falhas reais**, a partir dos sinais e violações registrados.

**Forma:** proposta de um módulo com dois binários e um núcleo comum (`facts`, `runs`, `store/open`), decidida na Fatia 0 do Scribo. Até lá, o contrato garante a conversa sem compartilhar código.

---

## 7. Limites declarados

Ditos com clareza para não gerar falsa confiança:

- Não criptografa nada em repouso. Use criptografia de disco do sistema.
- Não anonimiza conteúdo de arquivo. Um FASTQ é identificável por natureza.
- Não substitui aprovação de acesso. Colligo não sabe se você tem direito ao dado.
- Não protege contra o pipeline downstream. Se a próxima ferramenta escreve caminhos em log, o problema volta.
- Não faz análise, e a fronteira é deliberada: validamos que o arquivo é estruturalmente o que diz ser, nunca que o conteúdo faz sentido biológico. Regra prática: nenhuma checagem que exija alinhar reads. Strandedness, sexo, troca de amostra por genótipo e contaminação fina são do pipeline.
- **Não orquestra nem supervisiona execução.** Isso é do Nextflow ou do Snakemake, que já rodam cada tarefa em diretório próprio publicando saída só em caso de sucesso, registram comando, versões e checksums, medem recurso e fazem retry. Invólucro de comando, triagem de falha e vigia de job foram considerados e descartados por esse motivo. Um pipeline que usa Colligo mais um orquestrador é o caso previsto, não uma concessão.
