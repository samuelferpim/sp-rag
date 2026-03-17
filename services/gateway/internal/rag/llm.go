package rag

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

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
