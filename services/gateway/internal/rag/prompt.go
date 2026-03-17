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

func BuildPrompt(query string, chunks []Chunk) []openai.ChatCompletionMessage {
	var contextParts []string
	for i, chunk := range chunks {
		contextParts = append(contextParts, fmt.Sprintf(
			"[Excerpt %d] Source: %s, Page %d\n%s",
			i+1, chunk.SourceFile, chunk.Page, chunk.Text,
		))
	}

	contextBlock := strings.Join(contextParts, "\n\n---\n\n")

	userMessage := fmt.Sprintf("## Document Excerpts\n\n%s\n\n## Question\n\n%s", contextBlock, query)

	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userMessage},
	}
}
