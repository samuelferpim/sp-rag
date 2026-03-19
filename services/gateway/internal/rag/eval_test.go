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

func TestBuildEvaluationPrompt(t *testing.T) {
	chunks := []Chunk{
		{Text: "Go is a language.", SourceFile: "doc.pdf", Page: 1},
	}
	messages := BuildEvaluationPrompt("What is Go?", chunks, "Go is a programming language.")

	assert.Len(t, messages, 2)
	assert.Equal(t, openai.ChatMessageRoleSystem, messages[0].Role)
	assert.Contains(t, messages[0].Content, "strict factual evaluator")
	assert.Contains(t, messages[1].Content, "Go is a programming language.")
	assert.Contains(t, messages[1].Content, "What is Go?")
	assert.Contains(t, messages[1].Content, "doc.pdf")
}

func TestBuildRetryPrompt(t *testing.T) {
	chunks := []Chunk{
		{Text: "Go is a language.", SourceFile: "doc.pdf", Page: 1},
	}
	messages := BuildRetryPrompt("What is Go?", chunks, "Go was created in 2005.", "Creation date not in context")

	assert.Len(t, messages, 2)
	assert.Equal(t, openai.ChatMessageRoleSystem, messages[0].Role)
	assert.Contains(t, messages[0].Content, "flagged as not fully grounded")
	assert.Contains(t, messages[1].Content, "REJECTED")
	assert.Contains(t, messages[1].Content, "Creation date not in context")
}

func TestEvaluateAnswer_Grounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{
					Content: `{"is_grounded": true, "reason": "All claims verified."}`,
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	messages := BuildEvaluationPrompt("test?", []Chunk{{Text: "fact"}}, "fact-based answer")
	result, err := EvaluateAnswer(context.Background(), client, "gpt-4o-mini", messages)

	require.NoError(t, err)
	assert.True(t, result.IsGrounded)
	assert.Equal(t, "All claims verified.", result.Reason)
}

func TestEvaluateAnswer_NotGrounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{
					Content: `{"is_grounded": false, "reason": "Contains hallucinated date."}`,
				}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	messages := BuildEvaluationPrompt("test?", []Chunk{{Text: "fact"}}, "hallucinated answer")
	result, err := EvaluateAnswer(context.Background(), client, "gpt-4o-mini", messages)

	require.NoError(t, err)
	assert.False(t, result.IsGrounded)
	assert.Contains(t, result.Reason, "hallucinated")
}

func TestEvaluateAnswer_InvalidJSON_DefaultsNotGrounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{
			Choices: []openai.ChatCompletionChoice{
				{Message: openai.ChatCompletionMessage{Content: "This is not JSON"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	messages := BuildEvaluationPrompt("test?", []Chunk{{Text: "fact"}}, "answer")
	result, err := EvaluateAnswer(context.Background(), client, "gpt-4o-mini", messages)

	require.NoError(t, err)
	assert.False(t, result.IsGrounded)
}

func TestEvaluateAnswer_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	messages := BuildEvaluationPrompt("test?", []Chunk{{Text: "fact"}}, "answer")
	result, err := EvaluateAnswer(context.Background(), client, "gpt-4o-mini", messages)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestEvaluateAnswer_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openai.ChatCompletionResponse{Choices: []openai.ChatCompletionChoice{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = server.URL + "/v1"
	client := openai.NewClientWithConfig(cfg)

	messages := BuildEvaluationPrompt("test?", []Chunk{{Text: "fact"}}, "answer")
	result, err := EvaluateAnswer(context.Background(), client, "gpt-4o-mini", messages)

	assert.Error(t, err)
	assert.Nil(t, result)
}
