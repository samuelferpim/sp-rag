package rag

import (
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

type Chunk struct {
	Text       string
	SourceFile string
	Page       int
}

const systemPrompt = `You are a helpful assistant that answers questions based on the provided documents.
Use ONLY the information from the document excerpts below to answer the question.
If the answer cannot be found in the provided excerpts, say so clearly.
Always cite the source document and page number when referencing information.`

const evaluatorPrompt = `You are a strict factual evaluator for a RAG (Retrieval-Augmented Generation) system.
Your task is to determine if the provided answer is 100% grounded in the given context excerpts.

An answer is "grounded" if and only if:
- Every factual claim can be directly verified from the provided context
- No information is fabricated or hallucinated beyond what the context states
- No unsupported assumptions or inferences are made

An answer is NOT grounded if:
- It contains any claim not supported by the context
- It adds details, numbers, or facts not present in the excerpts
- It makes generalizations beyond what the context covers

You MUST respond with ONLY a valid JSON object, no extra text:
{"is_grounded": true, "reason": "All claims are supported by the provided excerpts."}
or
{"is_grounded": false, "reason": "Brief explanation of what is not grounded."}`

const retrySystemPrompt = `You are a helpful assistant that answers questions based on the provided documents.
Your previous answer was flagged as not fully grounded in the source material.

STRICT RULES:
- Use ONLY information explicitly stated in the document excerpts
- Do NOT add any information, assumptions, or inferences
- If the excerpts do not contain enough information, say so clearly
- Cite the source document and page number for every claim
- Prefer being incomplete over being inaccurate`

// buildContextBlock formats chunks into a numbered excerpt block.
func buildContextBlock(chunks []Chunk) string {
	var contextParts []string
	for i, chunk := range chunks {
		contextParts = append(contextParts, fmt.Sprintf(
			"[Excerpt %d] Source: %s, Page %d\n%s",
			i+1, chunk.SourceFile, chunk.Page, chunk.Text,
		))
	}
	return strings.Join(contextParts, "\n\n---\n\n")
}

// BuildPrompt creates the initial generation prompt from query and retrieved chunks.
func BuildPrompt(query string, chunks []Chunk) []openai.ChatCompletionMessage {
	contextBlock := buildContextBlock(chunks)
	userMessage := fmt.Sprintf("## Document Excerpts\n\n%s\n\n## Question\n\n%s", contextBlock, query)

	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userMessage},
	}
}

// BuildEvaluationPrompt creates the self-reflection prompt for the LLM-as-a-Judge.
func BuildEvaluationPrompt(query string, chunks []Chunk, draft string) []openai.ChatCompletionMessage {
	contextBlock := buildContextBlock(chunks)
	userMessage := fmt.Sprintf(
		"## Context Excerpts\n\n%s\n\n## Original Question\n\n%s\n\n## Answer to Evaluate\n\n%s",
		contextBlock, query, draft,
	)
	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: evaluatorPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userMessage},
	}
}

// BuildRetryPrompt creates a rewrite prompt after the evaluator rejected the draft.
func BuildRetryPrompt(query string, chunks []Chunk, previousDraft string, reason string) []openai.ChatCompletionMessage {
	contextBlock := buildContextBlock(chunks)
	userMessage := fmt.Sprintf(
		"## Document Excerpts\n\n%s\n\n## Question\n\n%s\n\n## Your Previous Answer (REJECTED)\n\n%s\n\n## Reason for Rejection\n\n%s\n\n## Instructions\n\nRewrite the answer using ONLY facts from the excerpts above. Do not add anything not explicitly stated.",
		contextBlock, query, previousDraft, reason,
	)
	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: retrySystemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userMessage},
	}
}
