# ADR-014: Portaria Vertical — Multi-channel Layout

## Status
Accepted

## Context
The Portaria ("Síndico Virtual") vertical targets condo residents asking
questions about the building's regimento interno. The same RAG core must
be reachable from two very different surfaces:

- **WhatsApp** (production) — asynchronous, Meta-signed webhooks, reply via
  Graph API, payload semantics we don't control.
- **Web chat** (admin/demo/QA) — synchronous JSON in/out over HTTP, needed
  for syndicate staff to test configurations before pointing a real number
  at the webhook.

Two questions had to be answered:

1. Does the Portaria product justify a new service/module/repo?
2. How do we share conversation logic (emergency bypass, fallback copy,
   tenant-scoped RAG) between channels without duplicating code or leaking
   channel semantics into the orchestrator?

## Decision

### No new service. One package per vertical, multiple channel sub-packages.

Portaria lives inside the existing Go gateway as
`internal/portaria/` with this shape:

```
internal/portaria/
├── service.go            # channel-agnostic Service + emergency detection
├── service_test.go
├── web/                  # HTTP + embedded chat UI (admin/demo)
│   ├── handler.go
│   ├── handler_test.go
│   └── chat.html         # embedded via go:embed
└── whatsapp/             # Meta Cloud API adapter (production)
    ├── webhook.go        # GET verify + POST receive
    ├── webhook_test.go
    ├── signature.go      # X-Hub-Signature-256 HMAC validation
    ├── signature_test.go
    └── client.go         # Meta Graph API "send text" client
```

The Service defines one method, `HandleMessage(ctx, tenantID, senderID, text)
-> Reply`. Every channel is a thin translator:

- **WhatsApp** → verify HMAC, resolve phone_number_id → tenant, call
  Service, push reply back via Meta Graph API.
- **Web** → JSON request → tenant from `X-Tenant-ID` header → call
  Service → JSON response.

The generic `/query` handler is untouched; Portaria routes are opt-in
behind `PORTARIA_ENABLED=true`.

### Service owns the portaria-specific semantics

Two behaviors are vertical-specific and live in the Service, not in the
orchestrator:

1. **Emergency bypass.** Words like `fogo`, `vazamento`, `socorro`, `SAMU`
   must never be handled by the LLM. The Service short-circuits to a
   "chamando a portaria humana" reply with the condo's phone number. See
   `IsEmergency` in `service.go`.
2. **Friendly fallback copy.** When the orchestrator returns ungrounded,
   we rewrite the reply in PT-BR with the condo's contact, instead of the
   generic English fallback.

Keeping these in the Service means the generic `/query` continues to
return the neutral copy, while Portaria always speaks "síndico voice."

### WhatsApp adapter design notes

- **HMAC first, parse later.** `ValidateSignature` runs on the raw body
  before JSON parsing — a tampered body with a matching `"object"` field
  must still be rejected.
- **Always 200 on parseable-but-unusable payloads.** Unknown
  `phone_number_id`, non-text messages, status updates — all return 200
  so Meta's aggressive retry policy doesn't amplify noise. We return 401
  only for signature failures (genuine bad actors) and 400 for
  unparseable JSON.
- **Tenant resolution via phone_number_id.** Each condo gets its own
  Meta `phone_number_id`; `PORTARIA_WHATSAPP_PHONE_MAP` maps IDs to
  tenants. Unknown IDs are dropped with a log warning — this webhook can
  be safely shared with future verticals.
- **Sender is an interface.** `whatsapp.Sender` lets tests verify what
  would be sent without a real Graph API call.

## Alternatives Considered

- **Separate `verticals-portaria/` Go module.** Duplicates cache, authz,
  config, orchestrator. No real isolation benefit — the vertical needs
  the same security invariants as the core. Rejected.
- **One "channels" package with a plugin interface.** Premature
  abstraction. We only have two channels and they're structurally
  different (webhooks vs request/response). A third channel can add its
  own sub-package without refactor.
- **Inject a per-vertical system prompt override into the orchestrator.**
  Considered for tone customization; deferred. For MVP the generic
  grounding-first system prompt is adequate — condo rules are short,
  factual, and the friendly tone lives in the Service-level fallback.

## Consequences

**Positive:**
- Zero duplication: Service wraps the existing orchestrator, channels
  wrap the Service.
- Web chat is free — same Service, different adapter.
- Tests cover each layer in isolation: Service unit tests mock the
  orchestrator; channel tests mock the Service.
- New verticals (Legal-Tech, Dev Teams) follow the same template.

**Negative:**
- Two channels means two code paths to keep in sync on tenant/emergency
  handling. The Service is the only source of truth; drift shows up as
  a missing call to `IsEmergency`.
- `PORTARIA_WHATSAPP_PHONE_MAP` is env-based; scaling past ~50 condos
  will want a DB-backed mapping with Redis cache.

## References

- `internal/portaria/service.go`
- `internal/portaria/web/handler.go`
- `internal/portaria/whatsapp/webhook.go`
- `verticals/portaria/README.md` — operational/onboarding guide
- ADR-005 (Permission-aware cache) — still applies: phone-as-user means
  cache keys include tenant + phone, so condo A and B never collide.
