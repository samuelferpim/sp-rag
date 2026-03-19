package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	openai "github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeOpenAIServer creates a test HTTP server that returns a fixed chat completion response.
func fakeOpenAIServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: content}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func fakeOpenAIClient(t *testing.T, content string) (*openai.Client, *httptest.Server) {
	t.Helper()
	server := fakeOpenAIServer(t, content)
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)
	return client, server
}

func TestSemanticRouter_ClassifySimple(t *testing.T) {
	client, server := fakeOpenAIClient(t, `{"complexity": "simples"}`)
	defer server.Close()

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "What port does Qdrant use?")

	require.NoError(t, err)
	assert.Equal(t, ComplexitySimple, result)
}

func TestSemanticRouter_ClassifyComplex(t *testing.T) {
	client, server := fakeOpenAIClient(t, `{"complexity": "complexa"}`)
	defer server.Close()

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "Explain how the auth flow works end-to-end")

	require.NoError(t, err)
	assert.Equal(t, ComplexityComplex, result)
}

func TestSemanticRouter_InvalidJSON_DefaultsToComplex(t *testing.T) {
	client, server := fakeOpenAIClient(t, `not valid json at all`)
	defer server.Close()

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "some query")

	require.NoError(t, err)
	assert.Equal(t, ComplexityComplex, result)
}

func TestSemanticRouter_UnknownComplexity_DefaultsToComplex(t *testing.T) {
	client, server := fakeOpenAIClient(t, `{"complexity": "media"}`)
	defer server.Close()

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "some query")

	require.NoError(t, err)
	assert.Equal(t, ComplexityComplex, result)
}

func TestSemanticRouter_EmptyChoices_DefaultsToComplex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{Choices: []openai.ChatCompletionChoice{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "some query")

	require.NoError(t, err)
	assert.Equal(t, ComplexityComplex, result)
}

func TestSemanticRouter_APIError_DefaultsToComplex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "some query")

	require.NoError(t, err)
	assert.Equal(t, ComplexityComplex, result)
}

func TestSemanticRouter_ExtraWhitespace_ParsesCorrectly(t *testing.T) {
	client, server := fakeOpenAIClient(t, `  {"complexity": "simples"}  `)
	defer server.Close()

	router := NewSemanticRouter(client, "gpt-4o-mini")
	result, err := router.Classify(context.Background(), "What is the install command?")

	require.NoError(t, err)
	assert.Equal(t, ComplexitySimple, result)
}
