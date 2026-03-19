package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// CallLLM sends a chat completion request and returns the text response.
func CallLLM(ctx context.Context, client *openai.Client, model string, messages []openai.ChatCompletionMessage) (string, error) {
	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.2,
		MaxTokens:   1024,
	})
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM returned no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

// EvaluationResult holds the LLM-as-a-Judge verdict.
type EvaluationResult struct {
	IsGrounded bool   `json:"is_grounded"`
	Reason     string `json:"reason"`
}

// EvaluateAnswer calls the LLM with evaluation messages and parses the JSON verdict.
// On parse failure, returns IsGrounded=false (safe default: treat unparseable as not grounded).
func EvaluateAnswer(ctx context.Context, client *openai.Client, model string, messages []openai.ChatCompletionMessage) (*EvaluationResult, error) {
	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0,
		MaxTokens:   200,
	})
	if err != nil {
		return nil, fmt.Errorf("evaluator API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("evaluator returned no choices")
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)

	var result EvaluationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("evaluator JSON parse failed, assuming not grounded",
			"error", err, "raw_response", raw)
		return &EvaluationResult{
			IsGrounded: false,
			Reason:     "evaluation response could not be parsed",
		}, nil
	}

	return &result, nil
}
