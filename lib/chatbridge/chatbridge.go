package chatbridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/sashabaranov/go-openai"
)

type ChatBridge struct {
	client   *openai.Client
	messages []openai.ChatCompletionMessage
	model    string
}

func New(token string, model string) ChatBridge {
	return ChatBridge{
		client: openai.NewClient(token),
		model:  model,
	}
}

func (c *ChatBridge) Chat(ctx context.Context, message string) (io.Reader, error) {
	slog.InfoContext(ctx, "chatbridge: sending message", slog.Any("message", message))

	c.messages = append(c.messages, openai.ChatCompletionMessage{
		Content: message,
		Role:    openai.ChatMessageRoleUser,
	})

	completion, err := c.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: c.messages,
	})

	if err != nil {
		return nil, fmt.Errorf(
			"chatbridge: failed to request chat completion: %w",
			err,
		)
	}

	pipeReader, pipeWriter := io.Pipe()

	go func() {
		defer completion.Close()

		for {
			resp, err := completion.Recv()
			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				pipeWriter.CloseWithError(
					fmt.Errorf(
						"chatbridge: %s failed to respond to chat completion: %w",
						c.model,
						err,
					),
				)
			}

			_, err = pipeWriter.Write([]byte(resp.Choices[0].Delta.Content))
			if err != nil {
				return
			}
		}

		pipeWriter.Close()
	}()

	return pipeReader, nil
}
