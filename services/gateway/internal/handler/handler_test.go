package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sp-rag-gateway/internal/config"
)

func newTestHandler() *Handler {
	return &Handler{
		Config: &config.Config{
			QueryTopK:           5,
			QueryTimeoutSeconds: 5,
		},
	}
}

func TestHealth_Returns200(t *testing.T) {
	h := newTestHandler()
	app := fiber.New()
	app.Get("/health", h.Health)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestUpload_NoFile_Returns400(t *testing.T) {
	h := newTestHandler()
	app := fiber.New()
	app.Post("/upload", h.Upload)

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestQuery_EmptyBody_Returns400(t *testing.T) {
	h := newTestHandler()
	app := fiber.New()
	app.Post("/query", h.Query)

	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestQuery_MissingQuery_Returns400(t *testing.T) {
	h := newTestHandler()
	app := fiber.New()
	app.Post("/query", h.Query)

	body := `{"user_id": "alice"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestQuery_MissingUserID_Returns400(t *testing.T) {
	h := newTestHandler()
	app := fiber.New()
	app.Post("/query", h.Query)

	body := `{"query": "what is distributed computing?"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestQuery_InvalidJSON_Returns400(t *testing.T) {
	h := newTestHandler()
	app := fiber.New()
	app.Post("/query", h.Query)

	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
