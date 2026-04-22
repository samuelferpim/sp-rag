# Portaria Vertical — Síndico Virtual

Assistente conversacional para moradores de condomínio, treinado
exclusivamente com o regimento interno do prédio. Responde pelo
**WhatsApp** (produção) e por um **chat web** (admin/QA/demo).

O core técnico vive em `services/gateway/internal/portaria/`. Esta pasta
é só operacional: onboarding, seeds, prompts de referência, docs.

## Arquitetura em 1 parágrafo

Mesmo gateway Go, mesmo orchestrator, mesmo SpiceDB, mesmo Qdrant,
mesmo Redis. O que muda é um `portaria.Service` que envelopa o
orchestrator, adiciona bypass de emergência e fallback PT-BR, e dois
canais plugáveis: `web/` (HTTP JSON + chat.html embutido) e
`whatsapp/` (webhook Meta Cloud API com validação HMAC). Cada condomínio
é um `tenant_id`; cada número do WhatsApp (`phone_number_id`) é
mapeado para um tenant. Veja [ADR-014](../../docs/adr/014-portaria-vertical.md).

## Variáveis de ambiente

```bash
# Ativa as rotas /portaria e /webhook/whatsapp
PORTARIA_ENABLED=true

# Mensagem de fallback/emergência sempre inclui esse contato
PORTARIA_HUMAN_CONTACT="(11) 99999-0000"

# Quantos chunks buscar no Qdrant (default: 5)
PORTARIA_TOP_K=5

# WhatsApp (todos obrigatórios para habilitar o webhook)
PORTARIA_WHATSAPP_VERIFY_TOKEN=<token-inventado-pra-o-handshake-da-Meta>
PORTARIA_WHATSAPP_APP_SECRET=<app-secret-da-Meta>
PORTARIA_WHATSAPP_ACCESS_TOKEN=<access-token-permanente-da-Meta>
# phone_number_id:tenant_id separado por vírgula, um por condomínio
PORTARIA_WHATSAPP_PHONE_MAP="123456789:ed-flores,987654321:res-morumbi"
```

Se `PORTARIA_ENABLED=true` mas faltar algum secret do WhatsApp, só o
chat web é habilitado — útil para desenvolvimento local sem credenciais
Meta.

## Como testar localmente

### 1. Subir a infra + gateway

```bash
make setup         # infra + topics + spicedb
export PORTARIA_ENABLED=true
export PORTARIA_HUMAN_CONTACT="(11) 99999-0000"
make gateway
```

### 2. Fazer seed do regimento de um condomínio

Por enquanto use o pipeline normal de upload (o worker Python já processa
o PDF):

```bash
curl -F "file=@regimento-ed-flores.pdf" \
     -F "user_id=sindico-flores" \
     -F "permissions=all" \
     -H "X-Tenant-ID: ed-flores" \
     http://localhost:8081/api/v1/documents/upload
```

### 3. Abrir o chat web

```
http://localhost:8081/portaria
```

Preenche `tenant_id = ed-flores` e um `sender_id` qualquer (ex.:
`morador-101`). O chat usa o mesmo orchestrator do `/query` — cache,
authz, grounding check, tudo aplicado.

### 4. Testar o webhook do WhatsApp (sem Meta real)

```bash
# Simula o payload Meta com assinatura HMAC válida
SECRET=$PORTARIA_WHATSAPP_APP_SECRET
BODY='{"object":"whatsapp_business_account","entry":[{"id":"e1","changes":[{"field":"messages","value":{"messaging_product":"whatsapp","metadata":{"phone_number_id":"123456789"},"messages":[{"from":"+5511988887777","id":"wamid.1","type":"text","text":{"body":"posso trazer pet?"}}]}}]}]}'
SIG="sha256=$(printf %s "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')"
curl -X POST http://localhost:8081/webhook/whatsapp \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$BODY"
```

Reply vai via Meta Graph API — em dev, a chamada falha (token inválido)
mas o webhook retorna 200 e você vê o processamento no log.

## Conectar a um número WhatsApp real

1. Criar app em [developers.facebook.com](https://developers.facebook.com)
   → WhatsApp product → gerar `phone_number_id` + `access_token` +
   `app_secret`.
2. Registrar webhook URL: `https://<gateway-público>/webhook/whatsapp`
   com `verify_token` igual ao `PORTARIA_WHATSAPP_VERIFY_TOKEN`.
3. Assinar apenas o campo `messages`.
4. Adicionar o par `phone_number_id:tenant_id` ao
   `PORTARIA_WHATSAPP_PHONE_MAP`.

## Testes

```bash
cd services/gateway
go test ./internal/portaria/... -v -count=1
```

Cobre: detecção de emergência (11 frases), mapping grounded/ungrounded,
bypass do orchestrator, validação HMAC (round-trip + tamper), handshake
Meta, payloads não-texto, phone_number_id desconhecido.

## Roadmap deste vertical

- [ ] Admin CLI: `make portaria-onboard TENANT=ed-flores FILE=...`
- [ ] Templates Meta aprovados para mensagens proativas
      (aviso de encomenda, assembleia, manutenção)
- [ ] Rate limiting por `sender_id` (não só por tenant)
- [ ] Painel de analytics: % emergências, % cache hit, top perguntas
- [ ] Voice note → Whisper → mesma pipeline
