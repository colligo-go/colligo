# Colligo — Backlog

Features conscientemente adiadas. **Não implementar nada daqui sem pedido explícito.**

Regra: todo item carrega uma frase de "o que destravaria". Sem ela, o item é capricho; com ela, é decisão adiada com gatilho.

---

## Reparo granular

Comando que pega um arquivo já completo, revalida bloco a bloco e rebaixa só os corrompidos via `Range`.

Status: a fundação já existe. A tabela `file_chunks` e o download por chunk individual são construídos para resume na Fatia 1, e carregam tudo que o reparo precisa. Só o gatilho do comando não é plugado.

Destrava quando: aparecer o primeiro caso real de arquivo corrompido em produção e a reverificação full virar cara o suficiente para justificar reparo cirúrgico.

---

## Aspera (ascp)

Transferência via protocolo Aspera, ganho de ordem de magnitude em FASTQ grande transcontinental.

Destrava quando: houver demanda real e recorrente de transferência transcontinental de arquivos grandes, e o usuário tiver o `ascp` configurado. É binário externo com licença e configuração de rede fora do nosso controle — alto custo de suporte.

---

## Suporte a Windows testado

A lib não usa nada que impeça Windows (paths via `filepath`), mas não será testado explicitamente na v1.

Destrava quando: houver usuário real em Windows e CI multiplataforma estável.

---

## Store PostgreSQL

Implementação alternativa da interface `Store` para escala de consórcio.

Status: a interface `Store` já é abstração desde o início. Manter a interface estreita, mas não investir em PostgreSQL especulativamente.

Destrava quando: um caso real exceder o que o SQLite confortavelmente atende (muito além de ~100 experimentos / ~1000 arquivos) E houver necessidade de escrita concorrente multi-processo.

---

## Tipos de dado — v2 e além

Candidatos: TCGA/GDC, scRNA-seq com validação de estrutura (10x/h5ad/Cellranger), CUT&RUN, Hi-C, e os que a v1 revelar.

Nota: `singlecell` entra na v1 como aquisição apenas. Validação de estrutura de output de single-cell é um item separado aqui.

GDC requer token de autenticação e arquivos controlados; GSA (BGI) tem API pouco documentada e instável — ambos merecem planejamento dedicado.

Destrava quando: a v1 estiver estável e o uso real indicar quais tipos faltam. Não decidir os 5 da v2 no papel agora.

---

## Resync por conteúdo (rolling hash)

Para o caso de corrupção que desloca bytes, re-sincronizar a grade por conteúdo estilo rsync, em vez de rebaixar do ponto do erro até o fim.

Destrava quando: o caso de deslocamento se mostrar comum o suficiente para justificar a complexidade. Hoje é raro e o comportamento atual (rebaixar a cauda) é correto, só não ótimo.

---

## CLI completo

Além de `fetch` e `verify` (que entram na v1), comandos como `export`, `status`, `resume`, inspeção do banco.

Destrava quando: o uso como lib estiver maduro e houver demanda por operação direta pela linha de comando.

---

## Prefetch especulativo de metadados

Otimização: buscar metadados de experimentos prováveis antes de o usuário pedir.

Destrava quando: profiling mostrar que latência de fetch de metadados é gargalo real. Improvável dado o fan-out paralelo já planejado.
