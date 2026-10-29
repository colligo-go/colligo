# Colligo — Backlog

Features conscientemente adiadas. **Não implementar nada daqui sem pedido explícito.**

Saíram do backlog e foram para o `ROADMAP.md`: `compat` (Fatia 6, sobre Sequence Collections — já existem refcompat, em Python e experimental, e o CompareReferences do GATK, também experimental; o buraco agora é um binário rápido integrado à identidade dos arquivos, não a ideia), o validador como porta separada (virou a porta leve, Fatia 1), módulo nf-core e receita bioconda (esboço na Fatia 2), e o posicionamento contra prior art (virou `RELATED.md`, escrito agora e não na publicação).

Regra: todo item carrega uma frase de "o que destravaria". Sem ela, o item é capricho; com ela, é decisão adiada com gatilho.

---

## Diagnósticos traduzidos

Frases do catálogo `diagnoses/` em português e outros idiomas, escolhidas por locale.

Destrava quando: houver usuário que precise da frase na própria língua; os identificadores no `--json` já são estáveis e não dependem do texto.

---

## `colligo gc` — coletor de lixo que entende proveniência

Responder "estes 8TB podem morrer porque a receita está registrada e refazer custa X horas".

Todo laboratório tem scratch com dezenas de terabytes que ninguém tem coragem de apagar, porque ninguém sabe o que é regenerável.

Status: não existe nada bom. O Snakemake tem `temporary()`, mas só dentro de um workflow que você controla e cuja regra não editou desde então. Centros de HPC têm purga por idade — ferramenta de administrador, que não entende o que o arquivo é. A pergunta por "um CCleaner para bioinformática" circula na comunidade sem resposta.

Destrava quando: houver alguns meses de `result_inputs` reais do Scribo, que é o histórico de uso que faltava — arquivo que nenhum resultado usa é candidato; arquivo cujo resultado pode ser refeito a partir de entradas sob custódia é regenerável; o resto é insubstituível. `derived_files` complementa para derivados construídos fora de pipeline.

Nota: é o maior buraco genuíno da lista — o item onde "ninguém tem isso" é mais literalmente verdade.

---

## `colligo scan` — desidentificação antes de compartilhar

Antes de subir um VCF para repositório público: contém variantes identificantes? O header do BAM tem nome de paciente? O nome do arquivo tem prontuário?

Status: ferramentaria fina, risco alto. Conecta diretamente com o regime de `CONFIDENTIALITY.md`, e é a contrapartida de saída dele — hoje o documento protege o dado que entra, não o que sai.

Destrava quando: houver compartilhamento efetivo de dado derivado de fonte restrita. Não antes: um scanner mal calibrado dá falsa confiança, que é pior que nenhum scanner.

---

## Identidade de amostra propagada

Token que viaja com a amostra por todas as etapas e é conferido em cada uma, para pegar o glob que embaralhou arquivos e fez o FASTQ da amostra A virar o BAM da amostra B.

Status: ferramentas genéticas (somalier, NGSCheckMate) pegam troca depois do fato, comparando os dados, e só para dado genômico. Ninguém pega no nível do encanamento, que é onde o erro acontece.

Caminho escolhido: não um token no encanamento, e sim o Scribo importando a saída do somalier ou do NGSCheckMate e devolvendo `identity_confirmed` ou `identity_mismatch` à custódia (`CONTRACT.md` §4). Pega a troca depois do fato, mas com ferramentas que já existem e sem mudar o pipeline.

Destrava quando: a Fatia 4 do Scribo estiver pronta. Nada a implementar no Colligo além de consumir o sinal (SPEC §14.1).

---

## Diff semântico de saídas de pipeline

Registrado sem gatilho, intencionalmente.

Você atualiza o STAR de 2.7.10 para 2.7.11 e reroda. Os resultados mudaram? Mudaram de forma que importa? Hoje isso é `md5sum` (que sempre difere, por timestamp em header) ou olhar gráfico e torcer.

Não é extensão do Colligo — é projeto separado, grande, com lógica específica por formato (BAM, VCF, matriz de contagem, conjunto de picos), separando ruído de versão de resultado diferente.

É o de maior valor científico entre tudo que foi considerado, e o único candidato real a paper além do próprio Colligo. Fica aqui como ideia registrada, não como item adiado: não tem gatilho porque não é decisão adiada, é outro projeto.

---


## Tipos `atacseq`, `methylation`, `wgs`

Perfis e schemas dos três tipos que saíram da v1.

Status: o padrão está validado pelas fatias anteriores. Cada um é uma repetição — definir as colunas de `*_samples` ao implementar, não antes.

Destrava quando: um projeto ativo precisar de um deles. Tipo construído especulativamente é tipo não testado contra realidade.

---

## Reparo granular

Comando que pega um arquivo já completo, revalida bloco a bloco e rebaixa só os corrompidos via `Range`.

Status: a fundação já existe. A tabela `file_groups`, o outboard Bao e o download por faixa são construídos para resume na Fatia 3, e `attest` na Fatia 4 já identifica os grupos divergentes. Só o gatilho do comando não é plugado.

Destrava quando: aparecer o primeiro caso real de arquivo corrompido em produção e a rebaixada completa virar cara o suficiente para justificar reparo cirúrgico.

---

## Resync por conteúdo (rolling hash)

Para o caso de corrupção que desloca bytes, re-sincronizar os grupos por conteúdo estilo rsync, em vez de rebaixar do ponto do erro até o fim.

Destrava quando: o caso de deslocamento se mostrar comum o suficiente para justificar a complexidade. Hoje é raro e o comportamento atual é correto, só não ótimo.

---

## Aspera (ascp)

Transferência via protocolo Aspera, ganho de ordem de magnitude em FASTQ grande transcontinental.

Destrava quando: houver demanda real e recorrente, e o usuário tiver o `ascp` configurado. É binário externo com licença e configuração de rede fora do nosso controle — alto custo de suporte. Nota: para dado de dbGaP, `adopt` cobre o caso sem precisar do protocolo.

---

## Suporte a Windows nativo testado

Decisão atual: desenvolvimento e teste em Linux ou WSL; Windows nativo compila e suporta só a porta leve; estudo restrito em Windows nativo é recusado (`CONFIDENTIALITY.md` §5.2). Este item é suporte completo à porta pesada em Windows nativo.

Destrava quando: houver usuário real em Windows nativo e CI Windows estável.

---

## Store PostgreSQL

Implementação alternativa da interface `Store` para escala de consórcio.

Status: a interface `Store` já é abstração desde o início, e `storetest` é o gate que qualquer implementação alternativa precisa passar. Manter a interface estreita, mas não investir especulativamente.

Destrava quando: um caso real exceder o que o SQLite confortavelmente atende E houver necessidade de escrita concorrente multi-processo.

---



## Tipos de dado — v2 e além

Candidatos: TCGA/GDC, GSA (BGI), CUT&RUN, Hi-C, validação de estrutura de output de single-cell, e os que a v1 revelar.

GDC requer token e arquivos controlados; GSA tem API pouco documentada e instável — ambos merecem planejamento dedicado, e ambos são candidatos naturais a `adopt` em vez de adapter.

Destrava quando: a v1 estiver estável e o uso real indicar quais faltam. Não decidir os 5 da v2 no papel agora.

---

## Prefetch especulativo de metadados

Otimização: buscar metadados de experimentos prováveis antes de o usuário pedir.

Destrava quando: profiling mostrar que latência de fetch de metadados é gargalo real. Improvável dado o fan-out paralelo já planejado.

---

## Ler o data lineage do Nextflow

Importar registros `lid://` do Nextflow como `derived_files`, e verificar depois os outputs que ele registrou. O lineage do Nextflow, experimental desde a 25.04, grava caminho, tamanho e checksum, mas não tem comando de verificação posterior.

Destrava quando: o lineage sair de experimental e houver uso real de Colligo junto com Nextflow. Reproduzir o algoritmo de checksum do Nextflow é pré-requisito a investigar.

---

## Organismo declarado × observado

Sketch MinHash de alguns milhares de reads comparado a sketches de referência pequenos responde "isto é mesmo humano?". Pega amostra rotulada com o organismo errado e contaminação grosseira, antes de qualquer alinhamento. sourmash e Mash já fazem o sketch; o novo é usá-lo como afirmação contra o metadado.

Alternativa já coberta: o sinal `suspect` do Scribo por desvio de taxa de alinhamento entre amostras pega o mesmo erro depois do pipeline, sem sketches e sem o Colligo cruzar a fronteira da análise. O MinHash continua útil para pegar o erro **antes** do alinhamento.

Destrava quando: houver um caso real de organismo trocado que o sinal pós-pipeline pegou tarde demais, e uma forma de distribuir os sketches de referência sem inchar o binário da porta leve.

---

## Acelerador Rust do extrator

Implementação em Rust do extrator de fatos, como processo separado, pelo contrato de SPEC §9 "Fronteira de substituição". Libdeflate ou zlib-ng para descompressão, parser de FASTQ sobre o buffer, mesmos fatos byte a byte que o extrator Go.

Regras, fixadas agora para a arquitetura nascer com a porta aberta:

- Processo separado. Nem cgo, nem WASM.
- Opcional. Nada quebra sem ele.
- Uma fronteira: o extrator. Rede, banco, adapters e CLI ficam só em Go.
- Um sentido: Go chama Rust.
- Um contrato: esquema de fatos com `extractor_version` compartilhada.
- Entra só com teste diferencial e fuzz contra o extrator Go.

Destrava quando: o benchmark de `TESTING.md` §7.1 mostrar, depois da troca para `klauspost/compress` e da distribuição paralela dos consumidores, que o `check` completo ainda fica acima da meta de 110% do piso por causa da descompressão.

Nota: ganha também como segunda implementação independente, a defesa de `RELIABILITY.md` M9.

---

## Descompressão paralela de gzip simples

Gzip simples não tem ponto de entrada no meio do fluxo, então não se paraleliza trivialmente. O rapidgzip mostra que descompressão especulativa paralela funciona e chega a várias vezes a velocidade de um núcleo.

Destrava quando: o benchmark de `TESTING.md` §7.1 mostrar que a descompressão de gzip simples é o gargalo dominante em uso real, depois de esgotadas a troca de biblioteca e a distribuição paralela dos consumidores.

---

## Criptografia em repouso

Envelopar o banco e os arquivos com chave gerenciada.

Status: fora de escopo por decisão, documentado em `CONFIDENTIALITY.md` §7. Criptografia de disco do sistema operacional resolve o mesmo problema melhor e não é nossa responsabilidade.

Destrava quando: houver requisito regulatório explícito que a criptografia de disco não satisfaça.
