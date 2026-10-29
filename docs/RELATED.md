# Colligo — Trabalhos relacionados

O que já existe, o que cada ferramenta resolve, e onde o Colligo se encaixa. Escrito cedo de propósito: posicionamento honesto força corte de escopo, e é melhor cortar antes de implementar.

Regra deste documento: toda afirmação de "ninguém faz X" tem data e é revisitada. O campo anda; `compat` já foi "ninguém tem" e deixou de ser.

Última revisão: 2026-09.

---

## 1. A tese em uma linha

As ferramentas abaixo garantem **bytes** ou **transferência**. O Colligo garante que o arquivo é **o que a fonte e você declararam** — estrutura, contagens, par, identidade, completude e status na fonte — e acompanha isso no tempo. Nenhuma peça isolada é nova. A combinação, com esse foco, é.

---

## 2. Download de dado público

| Ferramenta | O que faz | O que não faz |
| ---------- | --------- | ------------- |
| iSeq | GSA, SRA, ENA, DDBJ, GEO; confere tamanho e MD5; retoma download. Publicado em Bioinformatics, 2024 | Não valida estrutura, contagem declarada, par nem identidade; não vigia no tempo |
| nf-core/fetchngs | Metadados e FASTQ de SRA/ENA/DDBJ/GEO; gera samplesheet para pipelines nf-core | Idem; custódia termina no fim do workflow |
| ffq, pysradb, GEOfetch | Metadados e links de arquivo | Não baixam com custódia |
| enaBrowserTools, fastq-dl, kingfisher | Download de ENA/SRA por vários protocolos | Idem |
| q2-fondue | Aquisição dentro do QIIME 2 | Restrito ao ecossistema |
| sra-tools (`prefetch`, `fasterq-dump`) | Transferência e extração oficiais | Extração pode sair truncada com código 0 (caso QC Fail) |

Consequência para o Colligo: download não é diferencial. `fetch` existe porque a custódia precisa começar no primeiro byte, não para competir com iSeq.

---

## 3. Integridade de bytes no tempo

| Ferramenta | O que faz | O que não faz |
| ---------- | --------- | ------------- |
| git-annex | `fsck --incremental --incremental-schedule` verifica checksums de forma amortizada ao longo de semanas; detecta bitrot | Não sabe o que é FASTQ, GTF ou par; não conhece fonte de bioinformática |
| DataLad | Datasets versionados sobre git-annex; `download-url` registra origem; padrão em neuroimagem | Idem; proveniência é de URL, não de semântica |
| DVC | `import-url` grava ETag ou checksum; `update` detecta que a fonte mudou | Idem |
| ZFS, Btrfs, SnapRAID | Scrub por bloco no nível do sistema de arquivos | Abaixo da aplicação; sem noção de arquivo lógico nem de fonte |

Consequência: "reverificação amortizada com localização por bloco" **não** é contribuição do Colligo. A árvore BLAKE3 é infraestrutura, não tese. Quem já usa DataLad pode continuar usando; o Colligo acrescenta a camada semântica que o DataLad não tem.

---

## 4. Validação de formato

| Ferramenta | O que faz |
| ---------- | --------- |
| `samtools quickcheck` | Checa bloco EOF de BAM/CRAM |
| `gzip -t`, `bgzip` | Integridade de gzip |
| validadores de FASTQ (fastQValidator, fqtools e outros; lista no FAIR Cookbook) | Estrutura de FASTQ |
| `gffread`, `gt gff3validator` | GTF/GFF3 |
| `vcf-validator` | VCF |

Espalhados em linguagens e interfaces diferentes, e cada um olha um arquivo isolado, sem o que a fonte declarou. O Colligo usa a mesma leitura para estrutura, contagem e hash, e compara contra a fonte.

---

## 5. Identidade e compatibilidade de referência

| Ferramenta | O que faz | Estado |
| ---------- | --------- | ------ |
| refget Sequence Collections (GA4GH) | Padrão de identificadores derivados do conteúdo para coleções de sequências; comparação entre coleções | Padrão aprovado; implementação de referência em Python |
| refgenie | Gerencia assets de referência e índices | Maduro; o Colligo não compete — só registra release |
| refcompat | Compatibilidade entre FASTA, `.fai`, `.dict`, BAM/CRAM, VCF, GTF, BED por refget/SeqCol | Python, experimental, sem release |
| GATK CompareReferences | Compara FASTAs de referência | Experimental |

Consequência: `compat` não é ideia inédita. O que o Colligo oferece é um binário rápido em que compatibilidade é consulta sobre fatos já extraídos (`seqcol`, `coord_digest`), integrada à identidade e à custódia dos arquivos. `identify` — dizer qual release é um GTF órfão pelo conteúdo — não tem equivalente conhecido.

---

## 6. Proveniência de execução

| Ferramenta | O que faz | O que não faz |
| ---------- | --------- | ------------- |
| Nextflow data lineage | Desde a 25.04, experimental: registra caminho, tamanho e checksum de outputs; `lineage list/view/find/render/diff` | Não tem comando de verificação posterior; não cobre dado adquirido fora do workflow |
| Snakemake | Proveniência e `temporary()` dentro do workflow | Idem |

Consequência: `derived_files` fica mínimo. Dentro de workflow, proveniência é do orquestrador. Ler o lineage do Nextflow está no `BACKLOG.md`.

---

## 7. Adoção e desempenho

| Ferramenta | Relação com o Colligo |
| ---------- | --------------------- |
| nf-schema | Valida as colunas de uma samplesheet nf-core contra um JSON schema; não olha o conteúdo dos arquivos. `check --samplesheet` completa |
| MultiQC | Agrega relatórios de QC; o Colligo entra como módulo em vez de criar relatório próprio |
| Cell Ranger | Detecta a química 10x sozinho, dentro da execução; o Colligo compara a química observada com a declarada antes do pipeline |
| sourmash, Mash | Sketches MinHash; base possível para organismo declarado × observado (backlog) |
| DVC | Arquivos `.dvc` funcionam como lockfile de bytes; `colligo.lock` fixa identidades e contagens |
| rapidgzip | Descompressão paralela de gzip simples; referência de teto de desempenho (backlog) |
| aria2 | Download com várias conexões por arquivo; número fixo de conexões e verificação depois do download. O Colligo sobrepõe a verificação e adapta a concorrência |
| TLA+, TLC | Especificação e model checking do protocolo de resume; técnica comum em sistemas distribuídos e rara em ferramentas de bioinformática |

---

## 8. nf-core é canal, não concorrente

O nf-core é uma comunidade de pipelines em Nextflow; o fetchngs é um workflow que orquestra outras ferramentas. O Colligo é um binário que roda sozinho ou dentro de qualquer pipeline. As categorias são diferentes, e a relação desejada é de composição: o Colligo como módulo nf-core chamado antes do alinhamento, e `check --samplesheet` validando a entrada que os pipelines nf-core já recebem.

A linguagem não é o que separa as duas coisas. Go facilita a distribuição — binário estático, biocontainer trivial, inicialização instantânea — mas uma checagem isolada do Colligo pode ser reescrita em qualquer linguagem dentro de um workflow. O que não se reescreve rápido é a arquitetura de fatos por raiz e o benchmark de mutantes.

| Linguagem | Onde ganha | Onde perde para este projeto |
| --------- | ---------- | ---------------------------- |
| Go | rede, concorrência, binário estático, velocidade de desenvolvimento | descompressão sem SIMD; ecossistema de formatos menor, CRAM fraco |
| Rust | descompressão e parsing (libdeflate, zlib-ng, noodles, needletail) | async para rede, build mais pesado, desenvolvimento mais lento |
| C/C++ | teto de desempenho (htslib, rapidgzip) | segurança de memória, distribuição, manutenção por uma pessoa |
| Python | ecossistema, onde o fetchngs e o refcompat vivem | velocidade no caminho quente, distribuição fora de conda |

Decisão: Go para o produto, com porta estreita para um extrator Rust opcional (`DESIGN.md`, "Linguagem").

---

## 9. O que ninguém cobre junto

Até esta revisão, não se conhece ferramenta que faça:

- Comparar `read_count` e `base_count` publicados pela fonte contra o conteúdo do FASTQ (M15).
- Reconsultar a fonte e avisar que um run usado foi suprimido como incorreto (M16).
- Identificar release de GTF e coleção de sequências de FASTA pelo conteúdo, com um comando.
- Fazer tudo isso numa leitura só por arquivo, sem banco, com código de saída que falha fechado.
- Medir, num benchmark público de mutantes, quais falhas silenciosas cada ferramenta deixa passar.
- Comparar química de single-cell observada, e colunas de matriz suplementar, contra o metadado do GEO antes do pipeline.

É esta lista que o Colligo precisa provar, e é esta lista que o benchmark de `TESTING.md` §7 mede.

---

## Referências

- iSeq — github.com/BioOmics/iSeq; Bioinformatics 40(11), btae641, 2024
- git-annex fsck — git-annex.branchable.com/git-annex-fsck
- DataLad download-url — docs.datalad.org
- DVC import-url e update — doc.dvc.org/command-reference
- Refget Sequence Collections v1.0.0 — ga4gh.github.io/refget/seqcols
- refcompat — github.com/GregRR/refcompat
- GATK CompareReferences — gatk.broadinstitute.org
- Nextflow data lineage — docs.seqera.io/nextflow/tutorials/data-lineage
- ENA, política de supressão — ena-docs.readthedocs.io/en/latest/faq/release.html
- QC Fail, corrupção na extração do SRA — sequencing.qcfail.com
- FAIR Cookbook, validadores de FASTQ — fairplus.github.io/the-fair-cookbook
