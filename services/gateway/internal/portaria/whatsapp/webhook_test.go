package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sp-rag-gateway/internal/orchestrator"
	"sp-rag-gateway/internal/portaria"
)

type fakeOrch struct {
	result *orchestrator.QueryResult
}

func (f *fakeOrch) Execute(context.Context, string, string, string, int) (*orchestrator.QueryResult, error) {
	return f.result, nil
}

type recordingSender struct {
	mu   sync.Mutex
	sent []sentMsg
}
type sentMsg struct{ phoneID, to, body string }

func (r *recordingSender) SendText(_ context.Context, phoneNumberID, to, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, sentMsg{phoneNumberID, to, body})
	return nil
}

func newTestApp(cfg Config, groundedAnswer string) (*fiber.App, *recordingSender) {
	orch := &fakeOrch{result: &orchestrator.QueryResult{
		Answer: groundedAnswer, Grounded: true,
	}}
	svc := portaria.NewService(orch, 5, "(11) 9999-0000")
	sender := &recordingSender{}
	h := NewHandler(cfg, svc, sender)
	app := fiber.New()
	h.Register(app)
	return app, sender
}

func inboundPayload(phoneID, from, text string) []byte {
	p := webhookPayload{
		Object: "whatsapp_business_account",
		Entry: []entry{{
			ID: "entry1",
			Changes: []change{{
				Field: "messages",
				Value: value{
					MessagingProduct: "whatsapp",
					Metadata:         metadata{PhoneNumberID: phoneID},
					Messages: []wppMessage{{
						From: from, ID: "wamid.1", Type: "text",
						Text: struct {
							Body string `json:"body"`
						}{Body: text},
					}},
				},
			}},
		}},
	}
	b, _ := json.Marshal(p)
	return b
}

func TestVerify_Success(t *testing.T) {
	app, _ := newTestApp(Config{VerifyToken: "t0k3n"}, "")
	req := httptest.NewRequest("GET", "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=t0k3n&hub.challenge=42", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "42", string(body))
}

func TestVerify_WrongToken(t *testing.T) {
	app, _ := newTestApp(Config{VerifyToken: "t0k3n"}, "")
	req := httptest.NewRequest("GET", "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=bad&hub.challenge=42", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestVerify_EmptyVerifyTokenConfigRefuses(t *testing.T) {
	app, _ := newTestApp(Config{}, "")
	req := httptest.NewRequest("GET", "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=&hub.challenge=42", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 403, resp.StatusCode, "empty verify token in config must never pass")
}

func TestReceive_HappyPath_SendsReply(t *testing.T) {
	secret := "s3cr3t"
	cfg := Config{
		AppSecret:       secret,
		TenantByPhoneID: map[string]string{"phone-A": "ed-flores"},
	}
	app, sender := newTestApp(cfg, "Pets pequenos são permitidos.")
	body := inboundPayload("phone-A", "+5511988887777", "posso ter pet?")

	req := httptest.NewRequest("POST", "/webhook/whatsapp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", Sign(secret, body))
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	require.Len(t, sender.sent, 1)
	assert.Equal(t, "phone-A", sender.sent[0].phoneID)
	assert.Equal(t, "+5511988887777", sender.sent[0].to)
	assert.Contains(t, sender.sent[0].body, "Pets pequenos")
}

func TestReceive_EmergencyBypassesOrchestrator(t *testing.T) {
	secret := "s3cr3t"
	cfg := Config{
		AppSecret:       secret,
		TenantByPhoneID: map[string]string{"phone-A": "ed-flores"},
	}
	app, sender := newTestApp(cfg, "this should not be used")
	body := inboundPayload("phone-A", "+5511988887777", "fogo no 3º andar!!")

	req := httptest.NewRequest("POST", "/webhook/whatsapp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", Sign(secret, body))
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	require.Len(t, sender.sent, 1)
	assert.Contains(t, sender.sent[0].body, "emergência")
}

func TestReceive_BadSignature_401(t *testing.T) {
	cfg := Config{
		AppSecret:       "real-secret",
		TenantByPhoneID: map[string]string{"phone-A": "ed-flores"},
	}
	app, sender := newTestApp(cfg, "")
	body := inboundPayload("phone-A", "+5511988887777", "oi")

	req := httptest.NewRequest("POST", "/webhook/whatsapp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", Sign("wrong-secret", body))
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 401, resp.StatusCode)
	assert.Empty(t, sender.sent)
}

func TestReceive_UnknownPhoneID_200NoSend(t *testing.T) {
	secret := "s3cr3t"
	cfg := Config{
		AppSecret:       secret,
		TenantByPhoneID: map[string]string{"phone-A": "ed-flores"},
	}
	app, sender := newTestApp(cfg, "whatever")
	body := inboundPayload("phone-UNKNOWN", "+5511988887777", "oi")

	req := httptest.NewRequest("POST", "/webhook/whatsapp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", Sign(secret, body))
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode, "Meta retries on non-2xx; swallow unknown phones")
	assert.Empty(t, sender.sent)
}

func TestReceive_NonTextMessageSkipped(t *testing.T) {
	secret := "s3cr3t"
	cfg := Config{
		AppSecret:       secret,
		TenantByPhoneID: map[string]string{"phone-A": "ed-flores"},
	}
	app, sender := newTestApp(cfg, "")

	// Build payload with type=image (no text body) — we only support text.
	body := []byte(`{
      "object":"whatsapp_business_account",
      "entry":[{"id":"e1","changes":[{"field":"messages","value":{
        "messaging_product":"whatsapp",
        "metadata":{"phone_number_id":"phone-A"},
        "messages":[{"from":"+5511988887777","id":"wamid.9","type":"image"}]
      }}]}]
    }`)
	req := httptest.NewRequest("POST", "/webhook/whatsapp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", Sign(secret, body))
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Empty(t, sender.sent)
}
