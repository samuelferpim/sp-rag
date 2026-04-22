package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sp-rag-gateway/internal/middleware"
	"sp-rag-gateway/internal/orchestrator"
	"sp-rag-gateway/internal/portaria"
)

type fakeOrch struct {
	result *orchestrator.QueryResult
	err    error
}

func (f *fakeOrch) Execute(context.Context, string, string, string, int) (*orchestrator.QueryResult, error) {
	return f.result, f.err
}

func newApp(orch portaria.Orchestrator) *fiber.App {
	svc := portaria.NewService(orch, 5, "(11) 9999-0000")
	h := NewHandler(svc)
	app := fiber.New()
	app.Use(middleware.TenantResolver())
	h.Register(app, "/api/v1")
	return app
}

func post(t *testing.T, app *fiber.App, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/portaria/chat", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestChat_GroundedAnswer(t *testing.T) {
	orch := &fakeOrch{result: &orchestrator.QueryResult{
		Answer:   "Mudanças: segunda a sábado das 8h às 17h.",
		Grounded: true,
	}}
	app := newApp(orch)

	status, body := post(t, app, map[string]string{
		"sender_id": "morador-101", "message": "horário da mudança?",
	}, map[string]string{"X-Tenant-ID": "ed-flores"})

	assert.Equal(t, 200, status)
	assert.Equal(t, "llm", body["routed"])
	assert.Equal(t, true, body["grounded"])
	assert.Contains(t, body["text"], "segunda a sábado")
}

func TestChat_EmergencyRoutesToHuman(t *testing.T) {
	orch := &fakeOrch{}
	app := newApp(orch)

	status, body := post(t, app, map[string]string{
		"sender_id": "morador-101", "message": "Tem fogo no hall!!!",
	}, map[string]string{"X-Tenant-ID": "ed-flores"})

	assert.Equal(t, 200, status)
	assert.Equal(t, "human", body["routed"])
	assert.Contains(t, body["text"], "(11) 9999-0000")
}

func TestChat_MissingTenant400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"sender_id": "morador-101", "message": "oi",
	}, nil)
	assert.Equal(t, 400, status)
	assert.Contains(t, body["error"], "tenant_id")
}

func TestChat_InvalidTenant400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"sender_id": "morador-101", "message": "oi",
	}, map[string]string{"X-Tenant-ID": "bad tenant!"})
	assert.Equal(t, 400, status)
	assert.NotEmpty(t, body["error"])
}

func TestChat_MissingMessage400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"sender_id": "morador-101", "message": "",
	}, map[string]string{"X-Tenant-ID": "ed-flores"})
	assert.Equal(t, 400, status)
	assert.Contains(t, body["error"], "message")
}

func TestChat_MissingSender400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"sender_id": "", "message": "oi",
	}, map[string]string{"X-Tenant-ID": "ed-flores"})
	assert.Equal(t, 400, status)
	assert.Contains(t, body["error"], "sender_id")
}

func TestChat_OrchestratorError502WithFriendlyCopy(t *testing.T) {
	orch := &fakeOrch{err: errors.New("qdrant down")}
	app := newApp(orch)
	status, body := post(t, app, map[string]string{
		"sender_id": "morador-101", "message": "posso pet?",
	}, map[string]string{"X-Tenant-ID": "ed-flores"})
	assert.Equal(t, 502, status)
	assert.Equal(t, "human", body["routed"])
	assert.Contains(t, body["text"], "Não encontrei")
}

func TestServeUI_ReturnsHTML(t *testing.T) {
	app := newApp(&fakeOrch{})
	req := httptest.NewRequest("GET", "/portaria", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "Síndico Virtual")
}
