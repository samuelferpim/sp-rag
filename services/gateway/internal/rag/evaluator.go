package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// Evaluator judges whether a draft answer is grounded in the provided context.
// The orchestrator depends on this interface (not on a concrete LLM client) so
// that the self-reflection loop can be exercised in tests with a pure mock.
type Evaluator interface {
	Evaluate(ctx context.Context, query string, chunks []Chunk, draft string) (*EvaluationResult, error)
}

// EvaluationResult holds the LLM-as-a-Judge verdict.
type EvaluationResult struct {
	IsGrounded bool   `json:"is_grounded"`
	Reason     string `json:"reason"`
}

// LLMEvaluator is the production implementation: it calls an OpenAI-compatible
// chat model with the strict JSON evaluator prompt.
type LLMEvaluator struct {
	client *openai.Client
	model  string
}

// NewLLMEvaluator wires the evaluator to an OpenAI client + model name.
// The model should be one of the cheaper tiers — the evaluator runs on every
// Complex query and ideally does not dominate latency.
func NewLLMEvaluator(client *openai.Client, model string) *LLMEvaluator {
	return &LLMEvaluator{client: client, model: model}
}

// Evaluate builds the strict-JSON evaluator prompt and parses the verdict.
func (e *LLMEvaluator) Evaluate(ctx context.Context, query string, chunks []Chunk, draft string) (*EvaluationResult, error) {
	messages := BuildEvaluationPrompt(query, chunks, draft)
	return EvaluateAnswer(ctx, e.client, e.model, messages)
}

// EvaluateAnswer calls the LLM with evaluation messages and parses the JSON verdict.
// On parse failure, returns IsGrounded=false (safe default: treat unparseable as
// not grounded — better a fallback answer than a hallucinated one).
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
