# Colligo — Confiabilidade

Documento normativo. Catálogo dos modos de falha silenciosa e a defesa de cada um.

Um erro silencioso é aquele que não interrompe nada, não gera aviso, e produz resultado plausível. É a única classe de falha que consome meses de trabalho antes de ser descoberta — e às vezes é descoberta por um revisor, não por você.

O documento é organizado por modo de falha, não por componente, porque é assim que o problema aparece na vida real.

---

## 1. Postura: falhar fechado

`DESIGN.md` diz "nunca silencioso sobre problemas". A implementação ingênua disso é avisar e continuar, o que na prática produz avisos que ninguém lê.

Colligo falha fechado. Três severidades:

| Severidade  | Efeito                                                                         |
| ----------- | ------------------------------------------------------------------------------ |
| `fatal`     | Aborta imediatamente. Estado parcial salvo. Export impossível.                 |
| `blocking`  | Registrado. Outros arquivos seguem. Estudo **não** fica completo.               |
| `advisory`  | Registrado e impresso. Não afeta completude.                                    |

**Modo estrito é o default.** Nele, qualquer `blocking` pendente no fim da execução produz código de saída diferente de zero. `--permissive` deixa o código de saída em zero, mas **não esconde nada**: o export continua marcando o estudo como incompleto e cada linha continua carregando seu estado de custódia. Permissive muda se o pipeline para, nunca se a informação existe.

### Códigos de saída

| Código | Significado                                                          |
| ------ | -------------------------------------------------------------------- |
| 0      | Completo e verificado                                                |
| 1      | Erro de uso ou configuração (nada foi tentado)                        |
| 2      | `fatal`: integridade, identidade, autenticação, disco                 |
| 3      | Incompleto: `blocking` pendente                                       |
| 4      | Interrompido: cancelado, estado salvo, retomável                      |
| 5      | `check --quick`: nada encontrado no que foi lido, conteúdo não lido por inteiro |

**Alcance do `fatal`.** Falha da própria ferramenta (vetores conhecidos de hash, canário) aborta a execução inteira: nada mais é confiável. Falha `fatal` de um arquivo (ilegível, página HTML, checksum que não confere) encerra a verificação *daquele* arquivo; na porta leve os demais continuam sendo verificados, porque não há estado a salvar e parar esconderia o diagnóstico deles. O código de saída é o da pior severidade encontrada.

`--expected` com a mesma chave repetida (sem diferenciar maiúscula de minúscula, mesmo com valor igual) é erro de uso, código 1, antes de ler qualquer arquivo: dois valores para a mesma chave são ambíguos e deixar o último vencer esconderia um erro de cópia.

`--expected` com hash malformado também é erro de uso, código 1, antes de ler qualquer arquivo: MD5 tem 32 hexadecimais, SHA-256 e BLAKE3 têm 64, sem diferenciar maiúscula. Valor esperado malformado é erro de quem digitou, não evidência de arquivo corrompido, então nunca vira `checksum_mismatch` fatal.

`--cross-check` escreve `colligo.sha256` e `colligo.b3` no formato do GNU: se o nome tem `\` ou quebra de linha, a linha começa com `\` e o nome sai com `\\` e `\n`, o que `sha256sum -c` e `b3sum -c` entendem. Link simbólico para diretório nunca é seguido (evita ciclos com `--recursive`); sai como "symbolic link to a directory, not followed" e em `skipped_directory_links` no JSON, e não como arquivo não reconhecido.

O `5` existe porque `colligo check --quick x && pipeline` não pode seguir como se o arquivo tivesse sido verificado: um gzip truncado no meio passa no quick. O `0` fica reservado para "completo e verificado". Problema encontrado no quick continua saindo `2` ou `3`; `--permissive` baixa um `blocking` para o `5`, nunca para `0`.

Código 3 e 4 são distintos de propósito. `3` significa "terminou e está errado". `4` significa "não terminou". Um script que trata os dois igual perde a diferença que importa.

---

## 2. Catálogo de modos de falha

### M1 — Arquivo íntegro, mas errado

GTF do Ensembl 110 e do 111 são ambos válidos e passam qualquer checksum. Produzem resultados diferentes. Nenhuma verificação de bytes detecta.

**Defesa.** Arquivo de referência tem identidade além do hash: `assembly`, `release`, `source`, `asset_kind`. O pipeline declara o que espera; divergência é `fatal`, não aviso.

```toml
[reference]
assembly = "GRCh38"
release  = "GENCODE 44"
```

Um arquivo de referência adquirido sem `release` determinável fica com `confidence = inferred` e é `blocking`. Referência sem proveniência é inútil para reprodutibilidade — este é exatamente o problema que a literatura de proveniência de referência documenta.

A declaração é confirmada pelo conteúdo sempre que possível (`colligo identify`): digest de Sequence Collection para FASTA, cabeçalho `##description` para GTF do GENCODE, hash contra checksums oficiais para o Ensembl. Confirmado pelo conteúdo → `confidence = confirmed`. Conteúdo que desmente a declaração → `fatal`.

### M2 — A fonte trocou o arquivo

Mesma URL, conteúdo diferente. GEO reenvia suplementares; FTP do Ensembl reorganiza releases.

**Defesa.** Captura de `ETag` e `Last-Modified` no primeiro download, gravados em `files`. Em toda revisita:

- `If-Range` no resume, para detectar troca no meio.
- Conteúdo diferente na mesma URL **nunca sobrescreve** um arquivo `done`. Cria-se linha nova com `supersedes` apontando para a anterior, ambas preservadas, e a divergência é `blocking`.

Substituição silenciosa é impossível por construção: não existe caminho de código que faça UPDATE destrutivo em arquivo `done`.

**Arquivo que muda durante a leitura.** Mesmo caminho, conteúdo diferente, só que debaixo da própria leitura: um download ainda em curso, uma etapa de pipeline reescrevendo o arquivo. O `CheckFile` faz `fstat` no descritor aberto antes e depois da leitura e compara tamanho, mtime e ctime (com nanossegundos) e inode. Qualquer diferença produz `changed_during_read`, `blocking`, modo M2: o hash pode descrever bytes que nunca existiram juntos, então o arquivo não sai `verified`, não recebe hashes no relatório nem certificado, e as demais afirmações ficam `skipped` por ela. Depois da leitura o `CheckFile` também faz `stat` pelo caminho (função de `internal/fsaccess`) e compara inode e dispositivo com os do arquivo aberto: caminho que aponta para outro arquivo (`mv novo.fastq.gz reads.fastq.gz`, como terminam os downloads seguros) dá `Observed: replaced`, e caminho que sumiu dá `removed`. Fora do Linux só tamanho e mtime são comparados, e a troca por caminho não é vista. Sobrescrita no lugar que preserva tamanho, mtime e ctime (relógio parado, `utimensat` depois da escrita) não é vista.

### M3 — Página de erro salva como dado

Servidor devolve `200` com HTML de erro. Você fica com um `.fastq.gz` de 4 KB que é uma página web.

**Defesa.** Checagem de bytes mágicos no primeiro grupo, antes de gravar o segundo. Se o formato declarado é gzip, os dois primeiros bytes são `1f 8b`. `fatal` para o arquivo, e o `.part` é preservado para inspeção.

Custa microssegundos e pega uma quantidade desproporcional de falhas reais.

### M4 — Truncamento que passa batido

O pior modo. Um `.gz` cortado descomprime parcialmente. Muita ferramenta downstream aceita sem reclamar e produz números plausíveis.

**Defesa.** Validação estrutural de formato ao final de todo download, **depois** do checksum e **antes** do rename para o nome final. O objetivo é verificar que o arquivo termina onde deveria terminar.

Custo, dito com precisão: para BGZF, BAM e CRAM há marcador de fim, e a verificação é barata. Para gzip simples e FASTQ não há — o ISIZE só confere descomprimindo tudo, e contar linhas também. Nesses formatos a validação é uma leitura completa do conteúdo descomprimido. Por isso ela acontece **uma vez por raiz de hash e versão de validador** (`DESIGN.md`, "Fatos e afirmações"), e a mesma leitura extrai contagens e IDs usados por M5 e M15.

| Formato     | Prefixo         | Verificação de término                                                                                                                                   |
| ----------- | --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| gzip        | `1f 8b`         | último membro fecha; ISIZE bate com o descomprimido mod 2^32                                                                                             |
| bgzf        | `1f 8b` + XLEN  | cadeia de BSIZE contígua até o fim; bloco EOF presente                                                                                                   |
| bam         | `BAM\1` em bgzf | bloco EOF de bgzf presente                                                                                                                               |
| cram        | `CRAM`          | contêiner EOF presente                                                                                                                                   |
| fastq / .gz | `@`             | contagem de linhas múltipla de 4; última linha completa; linha em branco só depois do último registro, e então `advisory`; BOM UTF-8 no início rejeitado |
| fasta       | `>`             | termina com newline; sem linha de sequência truncada; linha de comentário `;` rejeitada; BOM UTF-8 no início rejeitado                                   |
| vcf / .gz   | `##fileformat`  | header presente; última linha tem contagem de campos válida                                                                                              |
| gtf / gff3  | texto           | toda linha de feature tem 9 campos separados por tab; BOM UTF-8 no início rejeitado                                                                      |
| hdf5 / h5ad | `\x89HDF\r\n`   | superbloco íntegro; raiz abre                                                                                                                            |
| pdb         | texto           | registro `END` presente                                                                                                                                  |
| mmcif       | `data_`         | último bloco fecha                                                                                                                                       |
| rds         | magic RDS       | prefixo apenas                                                                                                                                           |
| tsv / csv   | —               | contagem de campos consistente; última linha completa                                                                                                    |

Formato declarado vem do perfil ou do TOML. Inferido por extensão → `confidence = inferred` e `advisory`.

Falha de validação estrutural → status `invalid`, severidade `blocking`. O arquivo nunca chega a `done`, portanto nunca conta para completude, portanto nunca vaza para o pipeline como se estivesse bom.

**Se eu tivesse que escolher uma única defesa deste documento, seria esta.**

### M5 — Par R1/R2 desalinhado

Contagens diferentes de reads, ou arquivos de amostras diferentes pareados por engano. Produz alinhamento lixo sem erro nenhum.

**Defesa.** Quando ambos os membros de um `pair_id` chegam a validado:

- Contagem de registros idêntica.
- ID do primeiro e do último registro compatíveis descontando o sufixo de par (`/1`, `/2`, ` 1:`, ` 2:`, e o `.1`/`.2` do `fastq-dump --readids`, só na forma `<acesso>.<spot>.<1|2>`, como `SRR001666.1.1`). Um `.1` ou `.2` final fora desse formato é parte do ID: `SRR1.1` e `SRR1.2` são spots diferentes.

Divergência → `blocking` nos dois arquivos, com `pair_count` (contagens diferentes) ou `pair_ids` (primeiro ou último ID diferente). Nenhum dos dois vira `done`.

**Como os pares se formam.** Pelo nome, no mesmo diretório: mesmo radical, `_1`/`_2`, `_R1`/`_R2` ou `.1`/`.2`, e o mesmo número de pedaço (`_001`). `_R1_001` com `_R2_002` não pareia. Se dois arquivos disputam o mesmo mate (por exemplo `s_R1.fastq` e `s_R1.fastq.gz`), nenhum dos três recebe afirmação de par. Um mate em gzip e o outro em texto puro pareiam normalmente.

**Mate ausente.** Arquivo com sufixo de mate (`_R1`, `_2`, `.1`...) cujo outro mate não está entre os arquivos verificados recebe `pair_missing`, `advisory`, modo M5, com o nome do mate esperado. Só dispara quando a execução tem mais de um arquivo. Vale também para mates em diretórios diferentes e para `_R1_001` com `_R2_002`, que não pareiam.

**O que a invariante não pega.** Ela compara contagem e o primeiro e o último ID. Dois arquivos com o mesmo número de reads, o mesmo primeiro e o mesmo último ID, mas reads do meio trocados ou reordenados, passam como `verified`. Conferir todos os IDs exigiria guardar ou resumir a sequência de IDs na mesma leitura; está fora da fatia atual.

### M6 — Sucesso parcial com saída zero

47 de 50 arquivos baixados. O pipeline segue com 47. O resultado sai. Ninguém percebe.

**Defesa.** Completude é operação de primeira classe, não inspeção manual.

- No `fetch`, grava-se `expected_file_count` e `expected_set_hash` (hash do conjunto ordenado de identidades de arquivo retornado pela fonte).
- Um fetch posterior que produz conjunto diferente → `blocking` "conjunto da fonte mudou", com o diff em `discrepancies`.
- Completude = todo arquivo do estudo em `done` **e** contagem igual a `expected_file_count`.
- Com `[results]` declarado e o Scribo no mesmo banco, completude vai até "processado": todo resultado esperado presente e não obsoleto (`CONTRACT.md` §7).
- `colligo status --require-complete` sai com 3 se não estiver completo. É a linha que vai no topo do seu script de análise.
- Export de estudo incompleto **recusa** sem `--permissive`.

### M7 — Execução interrompida e esquecida

Ctrl+C, três dias passam, você volta e esqueceu que ficou estado parcial.

**Defesa.** `tool_runs` registra início, fim e código de saída. Uma execução sem registro de fim é interrompida. Na abertura do banco, **sem ser perguntado**, Colligo relata: quando foi, quantos arquivos ficaram fora de `done`, e o comando exato para retomar.

### M8 — Config mudou entre execuções

Metade do acervo adquirida com um conjunto de parâmetros, metade com outro. Silencioso e insidioso.

**Defesa.** `tool_runs.config_hash` — hash canônico da configuração efetiva, após precedência resolvida. Mudança de hash para o mesmo estudo → `advisory` na abertura, listando quais chaves mudaram. Mudança em chave que afeta integridade (tamanho de grupo, versão de extrator, política de checksum) → `blocking`.

### M9 — Bug na própria ferramenta

Se o cálculo de hash tem erro, tudo "verifica" e tudo está errado. Nenhuma defesa interna alcança isso, porque a defesa usa o código defeituoso.

**Defesa em camadas.**

1. **Vetores conhecidos na inicialização.** Vetores oficiais de BLAKE3, MD5 e SHA-256 rodados em toda abertura de banco. Falha → `fatal`, ferramenta ou ambiente suspeito. Custa microssegundos.
2. **Canário.** Cada estudo tem um arquivo canário de 128 KiB de conteúdo determinístico e hash conhecido, verificado em toda execução pelo mesmo caminho de código dos arquivos reais. Se o canário falha, o problema é ambiente ou ferramenta, não dado — e a mensagem diz isso, para você não sair caçando corrupção onde não há.
3. **Verificação cruzada externa.** `colligo check --cross-check` emite arquivos no formato de `sha256sum` e `b3sum`, para você conferir com ferramenta que não é a nossa. Na suíte de testes, quando `b3sum` está no PATH, os hashes são comparados contra ele.
4. **Verificador de certificado independente.** `tools/verify_cert.py` recalcula hashes e contagens sem código do Colligo e confere o certificado (SPEC §13). Roda na suíte sobre todo o corpus sintético.
5. **Segunda implementação do extrator, quando existir.** Se o acelerador Rust de `DESIGN.md` entrar, os fatos dele e os do extrator Go são comparados byte a byte na suíte, e `check --cross-extract` roda os dois sobre o mesmo arquivo em produção. Divergência → `fatal` com os dois valores, e a mensagem aponta a ferramenta, não o dado.

Duas implementações independentes da checagem crítica é a única defesa real contra bug na checagem.

### M10 — Espaço em disco esgotado no meio

`statfs` antes de começar não basta: outro processo pode consumir o disco durante uma transferência de horas.

**Defesa.** Pré-checagem com margem de 5% além do tamanho esperado. `ENOSPC` durante escrita é `fatal`, mas o caminho de erro **grava o offset seguro antes de abortar** — é o que torna "dado parcial tem valor" verdadeiro sob a falha que mais ameaça o dado parcial. Reverificação de espaço a cada 1 GiB transferido, com aviso em 95% de uso.

### M11 — Convenção de cromossomo divergente

`chr1` do UCSC/GENCODE contra `1` do Ensembl. Se as convenções não batem entre genoma e anotação, a quantificação falha silenciosamente: sem erro, só números errados.

**Defesa.** `chromosome_naming` é metadado de primeira classe de todo asset de referência, detectado por amostragem do arquivo. Registro de artefato derivado cujos pais divergem na convenção → `fatal`. Colligo **nunca converte automaticamente**; converter em silêncio é criar o erro que se tenta evitar.

Generalização: `colligo compat` compara o sistema de coordenadas de arquivos heterogêneos pelo digest `sorted_name_length_pairs` do padrão Sequence Collections, e devolve o diff de nomes quando não batem. Convenção de nome é o caso visível; comprimento divergente com o mesmo nome — build diferente — é o caso invisível que o digest também pega.

### M12 — Artefato derivado sem proveniência

Um GTF fundido de GENCODE com Dfam não foi baixado, foi construído. Se você não sabe qual versão de cada pai entrou, o resultado não é reproduzível — e a ausência é invisível.

**Defesa.** `derived_files` e `derived_parents`. Todo derivado registra pais, ferramenta construtora, versão dela e hash dos parâmetros. Derivado sem pai registrado é `blocking`. O manifesto exporta a árvore completa.

### M13 — Relógio errado

Timestamps são a espinha da auditoria. Relógio errado corrompe a ordem dos eventos sem corromper byte nenhum.

**Defesa.** Todo timestamp em UTC. Na abertura, se o horário atual é anterior ao último evento gravado, `blocking` com a diferença. Sem NTP, Colligo pelo menos detecta a inconsistência.

### M14 — Fixture apodrecida

A fonte mudou o formato de resposta; sua suíte segue verde porque testa contra a captura de dois anos atrás.

**Defesa.** Job de drift semanal, separado do CI de PR, que regrava fixtures contra as APIs reais e abre issue na divergência. Detalhe em `TESTING.md` §3.

### M15 — Conteúdo válido, mas menor do que a fonte declarou

Caso documentado: extração com `fastq-dump` produziu read 2 com 1 bp em vez de 98, terminou com código 0, e o FASTQ resultante é estruturalmente válido. Não existe MD5 da fonte para um arquivo extraído localmente, a estrutura passa, e o erro só aparece no trimming ou no alinhamento — quando aparece.

**Defesa.** O ENA publica `read_count` e `base_count` por run. A leitura que valida a estrutura do FASTQ já descomprime tudo, então conta reads e bases sem custo adicional. Soma de R1 e R2 comparada contra o declarado; divergência → `blocking`, com os dois números em `discrepancies`.

É a defesa mais barata por falha real pega deste catálogo: zero leitura extra, uma consulta de metadado que o fetch já faz.

### M16 — A fonte condenou o dado depois que você o baixou

Um run pode ser suprimido no ENA depois do download. Suprimido some da busca, mas continua recuperável por accession — o pipeline segue funcionando. Supressão permanente é para dado considerado incorreto sem perspectiva de correção. Nada no disco muda, então nenhuma verificação local detecta.

**Defesa.** `colligo recheck --upstream` reexecuta a parte de metadados dos adapters e compara com `raw_responses`: status do registro, conjunto de arquivos, checksums publicados. Run suprimido → `blocking`. Checksum publicado diferente do guardado → `blocking`, com a mesma lógica de `supersedes` de M2. Estudo com `recheck` mais antigo que `recheck_window` → `advisory` na abertura.


### M17 — Química de single-cell declarada errada

Declarar 10x v3 para um dado v2, ou o contrário, é erro comum de metadado. O pipeline roda, a whitelist de barcodes errada derruba a taxa de célula válida, e o resultado sai plausível e pior.

**Defesa.** Fato `barcode`: comprimento do R1 (26 bp no 10x v2, 28 bp no v3) e taxa de acerto de uma amostra de barcodes contra as whitelists conhecidas. Química observada diferente da declarada no metadado ou no TOML → `blocking`. O Cell Ranger detecta a química sozinho, mas dentro do pipeline; aqui a divergência aparece antes, contra o que o GEO declarou.

### M18 — Matriz do GEO incoerente com o próprio GEO

Arquivo suplementar de contagem com colunas trocadas, amostras faltando, nomes de coluna que não correspondem aos GSMs, ou IDs de gene de um namespace incompatível com o genoma declarado. A matriz é um TSV válido; nada quebra.

**Defesa.** Fato `matrix`: nomes de coluna, contagem de linhas e namespace de ID (Ensembl com versão, Ensembl sem versão, símbolo). Afirmações: colunas ⊆ amostras do estudo e cobrindo todas; namespace compatível com a referência declarada. Divergência de colunas → `blocking`; namespace inesperado → `advisory` com o namespace observado.

### M19 — O mesmo dado depositado duas vezes

Dois runs com conteúdo idêntico, ou lidos do mesmo flowcell e lane, entram como réplicas distintas numa meta-análise e inflam a evidência.

**Defesa.** Fatos são indexados pela raiz do hash, então conteúdo idêntico entre accessions aparece sem custo: `advisory` listando as accessions. Fato `read_header` (instrumento, run, flowcell, lane) aponta a suspeita mais sutil — arquivos diferentes vindos da mesma lane declarados como amostras diferentes — também como `advisory`, porque há desenhos legítimos que produzem isso.

### M20 — Queda no meio de uma escrita deixa estado que mente

Energia cai, o job é morto pelo escalonador, o disco some. Se a ordem das escritas estiver errada, a retomada encontra um arquivo com nome final que nunca foi verificado, um grupo marcado `verified` cujo conteúdo não chegou ao disco, ou um evento no banco sem o arquivo correspondente.

**Defesa em duas camadas.**

1. **Modelo formal.** O protocolo de resume, o rename e a máquina de estados são especificados em TLA+ e verificados por model checking antes da implementação: em nenhuma sequência de falhas e reinícios um arquivo chega a `done` sem conteúdo conferido, e nenhum grupo fica `verified` sem `fsync` do conteúdo que ele cobre.
2. **Crash em qualquer ponto.** A suíte enumera cada escrita, `fsync` e `rename` de um `get` e de um `fetch`, mata o processo em cada um deles, retoma, e confere as invariantes do modelo. `TESTING.md` §6.1.

Ordem obrigatória, derivada do modelo: gravar bytes → `fsync` do `.part` → marcar grupo `verified` → commit do banco. Rename do `.part` só depois de `fsync` e de todas as afirmações; `fsync` do diretório depois do rename.
---

## 3. Custódia visível

Estado de custódia por arquivo, sempre disponível e sempre exportado:

| Estado       | Significado                                                        |
| ------------ | ------------------------------------------------------------------ |
| `verified`   | Conteúdo lido e conferido, dentro da janela de scrub. O gate de `stat` nunca produz este estado |
| `stale`      | Já foi conferido; janela expirou ou baseline mudou                  |
| `unverified` | Nunca conferido por conteúdo                                        |
| `invalid`    | Conferência falhou; grupos divergentes ou afirmação falha conhecidos                   |
| `adopted`    | Sob custódia, sem checksum de origem para cruzar                    |
| `suspect`    | Somado a qualquer estado: o Scribo observou, depois do pipeline, resultado incoerente para esta entrada |

Todo export carrega `custody_state`, `last_verified_at` e `verified_fraction` por linha, mais um sidecar `.custody.json` com o resumo do estudo. Quem consome o TSV downstream vê o estado no arquivo que já está lendo, sem precisar consultar o banco.

`adopted` merece nota: um arquivo adotado que veio sem checksum publicado tem integridade **interna** garantida a partir do momento da adoção, mas Colligo não pode afirmar que ele chegou correto. A distinção é registrada em vez de dissolvida.

---

## 4. `colligo doctor`

Diagnóstico de ambiente. Roda em qualquer momento, e automaticamente antes de transferência longa.

- Espaço livre contra o esperado da fila
- Permissões de banco, WAL, SHM e diretório
- Caminho em diretório de sincronização de nuvem
- Versão do SQLite e PRAGMAs efetivos (lê de volta, não assume que o SET funcionou)
- Vetores conhecidos de hash
- Canário do estudo
- Sanidade do relógio
- Presença de credencial por fonte configurada — **presença apenas**, nunca valor
- Execução anterior interrompida
- `.part` e arquivo final no mesmo filesystem
- Conectividade e limite efetivo por fonte

Saída legível e `--json`. Zero identificadores, para poder ser colada em público.

---

## 5. Precedência das defesas

Quando defesas discordam, esta é a ordem. Existe para não haver dúvida na implementação.

1. Vetores conhecidos de hash — se falham, nada mais é confiável
2. Canário do estudo
3. Sanidade do relógio
4. Permissões e classificação
5. Identidade do arquivo (assembly, release, ETag, arquivo estável durante a leitura)
6. Checksum da fonte
7. Árvore BLAKE3
8. Validação estrutural de formato
9. Invariantes de par
10. Contagem declarada pela fonte
11. Química e coerência de matriz declaradas
12. Completude do conjunto
13. Status do registro na fonte
14. Duplicata entre accessions

Uma falha em nível mais alto torna as verificações abaixo dela sem sentido, e a mensagem de erro diz isso em vez de despejar dez falhas derivadas.
