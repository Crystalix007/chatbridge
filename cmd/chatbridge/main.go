package main

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/Crystalix007/chatbridge/lib/chatbridge"
	"github.com/sashabaranov/go-openai"
)

func main() {
	bridge := chatbridge.New(os.Getenv("OPENAI_API_KEY"), openai.GPT3Dot5Turbo1106)
	ctx := context.Background()

	inputReader := bufio.NewScanner(os.Stdin)

	for inputReader.Scan() {
		if err := inputReader.Err(); err != nil {
			slog.ErrorContext(ctx, "failed to read input", slog.Any("error", err))
			os.Exit(2)
		}

		response, err := bridge.Chat(ctx, inputReader.Text())
		if err != nil {
			slog.ErrorContext(ctx, "failed to chat", slog.Any("error", err))
			os.Exit(1)
		}

		io.Copy(os.Stdout, response)
	}
}
