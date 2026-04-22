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

	"sp-rag-gateway/internal/legal"
	"sp-rag-gateway/internal/middleware"
	"sp-rag-gateway/internal/orchestrator"
)

type fakeOrch struct {
	result *orchestrator.QueryResult
	err    error
}

func (f *fakeOrch) Execute(context.Context, string, string, string, int) (*orchestrator.QueryResult, error) {
	return f.result, f.err
}

func newApp(orch legal.Orchestrator) *fiber.App {
	svc := legal.NewService(orch, 6, true)
	h := NewHandler(svc)
	app := fiber.New()
	app.Use(middleware.TenantResolver())
	h.Register(app, "/api/v1")
	return app
}

func post(t *testing.T, app *fiber.App, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/legal/query", bytes.NewReader(b))
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

func TestQuery_GroundedAnswer(t *testing.T) {
	orch := &fakeOrch{result: &orchestrator.QueryResult{
		Answer:   "Conforme o Art. 150 da Lei 8.666/1993...",
		Grounded: true,
		Sources: []orchestrator.Source{{
			FileName: "lei-8666.pdf",
			Entities: map[string][]string{"articles": {"150"}, "laws": {"8.666/1993"}},
		}},
	}}
	app := newApp(orch)
	status, body := post(t, app, map[string]string{
		"user_id": "advogado-1", "query": "art 150?",
	}, map[string]string{"X-Tenant-ID": "esc-a"})

	assert.Equal(t, 200, status)
	assert.Equal(t, "grounded", body["status"])
	assert.Contains(t, body["answer"], "Art. 150")
}

func TestQuery_UnverifiedCitationInStrictReturns200WithWarning(t *testing.T) {
	orch := &fakeOrch{result: &orchestrator.QueryResult{
		Answer:   "A Lei 9999/2099 determina...",
		Grounded: true,
		Sources: []orchestrator.Source{{
			Entities: map[string][]string{"laws": {"8.666/1993"}},
		}},
	}}
	app := newApp(orch)
	status, body := post(t, app, map[string]string{
		"user_id": "advogado-1", "query": "?",
	}, map[string]string{"X-Tenant-ID": "esc-a"})

	assert.Equal(t, 200, status)
	assert.Equal(t, "unverified_citations", body["status"])
	uns, ok := body["unverified_citations"].([]any)
	require.True(t, ok)
	assert.Len(t, uns, 1)
}

func TestQuery_MissingTenant400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"user_id": "u", "query": "q",
	}, nil)
	assert.Equal(t, 400, status)
	assert.Contains(t, body["error"], "tenant_id")
}

func TestQuery_MissingUser400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"user_id": "", "query": "q",
	}, map[string]string{"X-Tenant-ID": "esc-a"})
	assert.Equal(t, 400, status)
	assert.Contains(t, body["error"], "user_id")
}

func TestQuery_MissingQuery400(t *testing.T) {
	app := newApp(&fakeOrch{})
	status, body := post(t, app, map[string]string{
		"user_id": "u", "query": "",
	}, map[string]string{"X-Tenant-ID": "esc-a"})
	assert.Equal(t, 400, status)
	assert.Contains(t, body["error"], "query")
}

func TestQuery_OrchestratorError502(t *testing.T) {
	orch := &fakeOrch{err: errors.New("down")}
	app := newApp(orch)
	status, body := post(t, app, map[string]string{
		"user_id": "u", "query": "q",
	}, map[string]string{"X-Tenant-ID": "esc-a"})
	assert.Equal(t, 502, status)
	assert.Equal(t, "orchestrator_error", body["status"])
}

func TestServeUI_ReturnsHTML(t *testing.T) {
	app := newApp(&fakeOrch{})
	req := httptest.NewRequest("GET", "/legal", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	b, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(b), "Guardião Jurídico")
}
