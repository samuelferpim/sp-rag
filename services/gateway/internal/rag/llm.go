package rag

import (
	"context"

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

	return resp.Choices[0].Message.Content, nil
}
