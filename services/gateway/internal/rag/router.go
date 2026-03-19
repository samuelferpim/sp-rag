package rag

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

// Complexity represents the classified intent of a user query.
type Complexity string

const (
	ComplexitySimple  Complexity = "simples"
	ComplexityComplex Complexity = "complexa"
)

// Router classifies user queries by complexity to select the appropriate LLM model.
type Router interface {
	Classify(ctx context.Context, query string) (Complexity, error)
}

const classificationPrompt = `You are a query complexity classifier for an internal document search system.
Analyze the user's query and classify it as "simples" or "complexa".

SIMPLE ("simples") — the query asks for a direct, factual piece of information:
- A specific command, link, port number, or configuration value
- A short definition or a yes/no answer
- Something that can be answered from a single passage
- Examples: "What is the install command?", "Which port does Qdrant use?", "Where is the repo link?"

COMPLEX ("complexa") — the query requires reasoning, analysis, or synthesis:
- Explanations of why something happens or how a flow works end-to-end
- Comparisons, summaries, or cross-referencing multiple documents
- Troubleshooting, root-cause analysis, or recommendations
- Examples: "Why is this error occurring?", "Compare laws X and Y", "How does the auth flow work?"

Respond with ONLY a valid JSON object, no extra text:
{"complexity": "simples"}
or
{"complexity": "complexa"}`

// classificationResponse is the expected JSON structure from the LLM.
type classificationResponse struct {
	Complexity string `json:"complexity"`
}

// SemanticRouter uses an LLM to classify query complexity.
type SemanticRouter struct {
	client *openai.Client
	model  string
}

// NewSemanticRouter creates a router that classifies queries via the given model.
func NewSemanticRouter(client *openai.Client, model string) *SemanticRouter {
	return &SemanticRouter{client: client, model: model}
}

// Classify sends the query to the LLM and parses the complexity classification.
// On any error (API failure, malformed JSON), it defaults to ComplexityComplex
// to ensure the best model handles ambiguous cases.
func (r *SemanticRouter) Classify(ctx context.Context, query string) (Complexity, error) {
	resp, err := r.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: r.model,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: classificationPrompt},
			{Role: openai.ChatMessageRoleUser, Content: query},
		},
		Temperature: 0,
		MaxTokens:   50,
	})
	if err != nil {
		slog.Warn("router classification failed, defaulting to complex", "error", err)
		return ComplexityComplex, nil
	}

	if len(resp.Choices) == 0 {
		slog.Warn("router returned no choices, defaulting to complex")
		return ComplexityComplex, nil
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)

	var result classificationResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("router JSON parse failed, defaulting to complex",
			"error", err, "raw_response", raw)
		return ComplexityComplex, nil
	}

	switch Complexity(result.Complexity) {
	case ComplexitySimple:
		return ComplexitySimple, nil
	case ComplexityComplex:
		return ComplexityComplex, nil
	default:
		slog.Warn("router returned unknown complexity, defaulting to complex",
			"value", result.Complexity)
		return ComplexityComplex, nil
	}
}
