# Legal-Tech Vertical — Guardião Tributário e Jurídico

Motor de busca e análise para escritórios de advocacia e contabilidade
consumindo DOU, normativas da Receita Federal e legislação tributária.
Responde perguntas **citando obrigatoriamente Artigo, Lei e Decreto** e
**verifica cada citação** contra as entidades extraídas dos documentos
indexados — uma resposta com "Lei 9.999/2099" inventada é rejeitada
automaticamente.

O core técnico vive em `services/gateway/internal/legal/` e
`services/worker/app/etl.py` (extração de entidades). Esta pasta é
operacional: ingestão, prompts de referência, seeds, docs.

## Por que este vertical é diferente

Aluc­inação de identificador legal é um vetor de erro grave no domínio
jurídico. O SP-RAG já tem:

- **Smart chunking** (ADR-011) que preserva artigos inteiros.
- **Entity extraction** no worker — regex PT-BR captura `articles`,
  `laws`, `decrees`, `cnpjs` e grava no payload Qdrant.
- **Self-reflection grounding** (ADR-013) em linguagem natural.

O vertical legal adiciona a peça que faltava: **verificação
determinística de citações** contra as entidades dos chunks retornados.
Se o LLM escrever "Lei 9.999/2099" e essa string não aparecer sob
`laws` em nenhum chunk, a resposta é flagada (modo `lenient`) ou
substituída por um fallback auditável (modo `strict`, default).
Ver [ADR-015](../../docs/adr/015-legal-vertical.md).

## Variáveis de ambiente

```bash
# Ativa /legal e /api/v1/legal/query
LEGAL_ENABLED=true

# Quantos chunks buscar (legal tende a precisar de mais contexto — leis
# se referenciam entre si). Default: 6.
LEGAL_TOP_K=6

# Modo de verificação de citação:
#   true  (default) — respostas com citações não confirmadas viram fallback
#   false           — UI recebe as citações + lista de não-confirmadas pra flagar inline
LEGAL_STRICT_CITATION=true
```

## Como testar localmente

### 1. Infra + gateway

```bash
make setup
export LEGAL_ENABLED=true
make gateway
```

### 2. Indexar um PDF de lei

```bash
# Baixe uma lei qualquer (ex.: Lei 8.666/1993)
curl -o /tmp/lei-8666.pdf "https://example.com/lei-8666.pdf"

curl -F "file=@/tmp/lei-8666.pdf" \
     -F "user_id=advogado-1" \
     -F "permissions=all" \
     -H "X-Tenant-ID: escritorio-alpha" \
     http://localhost:8081/api/v1/documents/upload
```

O worker roda smart chunking + entity extraction e popula os chunks
no Qdrant com `entities.laws=["8.666/1993"]`, `entities.articles=["1","2",...]`.

### 3. Abrir o chat jurídico

```
http://localhost:8081/legal
```

Tenant: `escritorio-alpha`. User: `advogado-1`. Pergunte "Qual o limite
da dispensa no art. 24 da Lei 8.666/1993?".

Na resposta você verá:
- Pill verde ✓ ao lado de cada citação confirmada
- Pill vermelho ✗ em citações não confirmadas (se houver)
- Lista de fontes (arquivo + página + score)

### 4. API pra integração externa

O mesmo endpoint que o chat usa — integradores chamam direto:

```bash
curl -X POST http://localhost:8081/api/v1/legal/query \
  -H "Content-Type: application/json" \
  -H "X-Tenant-ID: escritorio-alpha" \
  -d '{"user_id":"advogado-1","query":"limite do art 24 da lei 8666?"}'
```

Resposta:

```json
{
  "answer": "Conforme o Art. 24 da Lei 8.666/1993, o limite é de...",
  "status": "grounded",
  "sources": [
    {"file_name":"lei-8666.pdf","page":4,"score":0.87,
     "entities":{"articles":["24"],"laws":["8.666/1993"]}}
  ],
  "citations": {"articles":["24"],"laws":["8.666/1993"]},
  "unverified_citations": [],
  "cached": false,
  "grounded": true,
  "model": "gpt-4o-mini",
  "timing": { "total_ms": 1420, "..." : "..." }
}
```

Status possíveis:

| Status | Significado |
|---|---|
| `grounded` | resposta verificada, todas citações confirmadas |
| `unverified_citations` | modo strict: fallback emitido; modo lenient: resposta com flags |
| `ungrounded` | orchestrator não conseguiu fundamentar — fallback |
| `no_context` | Qdrant não retornou nada relevante |
| `orchestrator_error` | falha upstream (Qdrant/Redis/OpenAI) |

## Testes

```bash
cd services/gateway
go test ./internal/legal/... -v -count=1
```

Cobre:
- 13 testes de extração de citações (articles, laws, decrees, CNPJs,
  dedup, ordinais `7º`, numeração composta `150-A`, `7.1`, trailing dot).
- 9 testes de Service (grounded happy path, hallucinated law strict/lenient,
  ungrounded do orchestrator, sem sources, answer sem citações, erro
  upstream, validação).
- 8 testes de handler web (grounded, unverified 200+warning, 400s,
  502 em erro, UI HTML).

## Roadmap do vertical

### Fase 1 (MVP — concluída)
- [x] Verificação de citação contra entities dos chunks
- [x] Web chat + endpoint JSON
- [x] Modo strict/lenient
- [x] ADR 015

### Fase 2 (automação de ingestão)
- [ ] **DOU fetcher Python**: cron diário baixa seções 1 e 3 do Diário
      Oficial via API da [Imprensa Nacional](https://www.in.gov.br/leiturajornal)
      e publica Kafka `document.uploaded` — mesmo contrato do upload manual.
- [ ] Classificador simples (regex + cabeçalho) filtrando atos relevantes
      por tenant (CNAE, área).
- [ ] **Receita Federal scraper**: normativas + instruções normativas.

### Fase 3 (qualidade)
- [ ] Prompt sistema específico: "cite SEMPRE Art./Lei/Decreto entre
      colchetes; nunca interprete — reproduza texto + cite onde consultar".
      Requer suporte a override de prompt no orchestrator (ver roadmap
      do core).
- [ ] Suporte a jurisprudência: HC, RE, REsp, AgRg, novo regex + entity.
- [ ] Confidence score por citação (quantos chunks confirmam).

### Fase 4 (GraphRAG)
- [ ] Promover entities Qdrant → Neo4j (nós: Lei, Artigo, Decreto).
- [ ] Hybrid retrieval: vector + graph walk ("quais decretos alteram o
      Art. X da Lei Y").
- [ ] Evaluator consulta o grafo como fonte adicional de verdade.

## Pontos de extensão

- **Novo tipo de entidade** (ex.: Súmula, Enunciado): adicione regex em
  `etl.py` `_SUMULA_RE` + Go `legal/citations.go` `sumulaRE`. Testes de
  parity em ambos os lados.
- **Canal novo** (Slack bot pra escritórios, plugin Office): crie
  `internal/legal/<canal>/` chamando `legal.Service.HandleQuery`. A
  Service é a interface estável.
