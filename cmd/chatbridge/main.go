package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Crystalix007/chatbridge/lib/chatbridge"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/go-errors/errors"
	"github.com/sashabaranov/go-openai"
	"golang.org/x/sync/errgroup"
)

const (
	pendingPrompt = "ChatGPT is writing..."
	readyPrompt   = "..."

	host = "::"
	port = "2024"
)

type model struct {
	chatBridge chatbridge.ChatBridge

	viewport viewport.Model
	textArea textarea.Model

	ctx                  context.Context
	err                  error
	streamingCompletions io.Reader
}

type completionUpdate struct{}
type completionsFinished struct{}

func main() {
	srv, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithMiddleware(
			bubbletea.Middleware(teaHandler),
			activeterm.Middleware(),
		),
	)
	if err != nil {
		slog.Error("failed to create server", slog.Any("error", err))

		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errg, ctx := errgroup.WithContext(ctx)

	errg.Go(srv.ListenAndServe)

	slog.Info("listening for connections", slog.String("address", srv.Addr))

	<-ctx.Done()

	slog.Info("shutting down server")

	if err := ctx.Err(); !errors.Is(err, ssh.ErrServerClosed) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			slog.Error("failed to shutdown server", slog.Any("error", err))
		}
	}

	if err := errg.Wait(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		slog.Error("failed to listen and serve", slog.Any("error", err))
	}
}

func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
	// This should never fail, as we are using the activeterm middleware.
	// pty, _, _ := s.Pty()

	// When running a Bubble Tea app over SSH, you shouldn't use the default
	// lipgloss.NewStyle function.
	// That function will use the color profile from the os.Stdin, which is the
	// server, not the client.
	// We provide a MakeRenderer function in the bubbletea middleware package,
	// so you can easily get the correct renderer for the current session, and
	// use it to create the styles.
	// The recommended way to use these styles is to then pass them down to
	// your Bubble Tea model.
	// renderer := bubbletea.MakeRenderer(s)

	return newModel(s.Context()), []tea.ProgramOption{tea.WithAltScreen()}
}

func newModel(ctx context.Context) *model {
	textArea := textarea.New()
	textArea.Placeholder = readyPrompt
	textArea.Focus()
	textArea.SetHeight(2)
	textArea.CharLimit = 256
	textArea.MaxWidth = 100
	textArea.ShowLineNumbers = false
	textArea.KeyMap.InsertNewline.SetEnabled(false)

	viewport := viewport.New(100, 20)
	viewport.SetContent("Welcome to ChatBridge. Type something and press enter to chat.")

	return &model{
		chatBridge: chatbridge.New(os.Getenv("OPENAI_API_KEY"), openai.GPT3Dot5Turbo1106),
		ctx:        ctx,
		textArea:   textArea,
		viewport:   viewport,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCMD tea.Cmd
		vpCMD tea.Cmd
	)

	m.textArea, tiCMD = m.textArea.Update(msg)
	m.viewport, vpCMD = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			fmt.Printf("%s\n", m.chatBridge.Messages())

			return m, tea.Quit
		case tea.KeyEnter:
			msg := m.textArea.Value()

			response, err := m.chatBridge.Chat(m.ctx, msg)
			if err != nil {
				m.err = fmt.Errorf("failed to chat: %w", err)

				return m, nil
			}

			m.textArea.Placeholder = pendingPrompt
			m.textArea.Reset()

			if m.streamingCompletions == nil {
				m.streamingCompletions = response
			} else {
				m.streamingCompletions = io.MultiReader(m.streamingCompletions, response)
			}

			return m, tea.Batch(m.AwaitNextStreamingUpdate, tiCMD, vpCMD)
		}

	case completionUpdate:
		m.viewport.SetContent(m.chatBridge.Messages())
		m.viewport.GotoBottom()

		return m, tea.Batch(m.AwaitNextStreamingUpdate, tiCMD, vpCMD)

	case completionsFinished:
		m.streamingCompletions = nil
		m.textArea.Placeholder = readyPrompt

	case error:
		m.err = msg

		return m, nil
	}

	return m, tea.Batch(tiCMD, vpCMD)
}

func (m model) View() string {
	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.viewport.View(),
		m.textArea.View(),
		"(esc to quit)",
	)
}

func (m *model) AwaitNextStreamingUpdate() tea.Msg {
	buf := make([]byte, 1024)

	_, err := m.streamingCompletions.Read(buf)
	if err != nil {
		return completionsFinished{}
	}

	return completionUpdate{}
}
