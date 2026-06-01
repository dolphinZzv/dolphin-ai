package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"dolphin/internal/common"

	"github.com/chzyer/readline"
)

func init() {
	Register("stdio", func(ctx context.Context, cfg map[string]any) (IO, error) {
		user := os.Getenv("USER")
		if user == "" {
			user = "unknown"
		}
		return NewStdio(user), nil
	})
}

type Stdio struct {
	*SessionHolder
	id     string
	rl     *readline.Instance
	reader *bufio.Reader
	mu     sync.Mutex
	user   string
	ctx    context.Context
	cancel context.CancelFunc
}

func NewStdio(user string) *Stdio {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Stdio{
		SessionHolder: NewSessionHolder(nil),
		id:            "stdio",
		reader:        bufio.NewReader(os.Stdin),
		user:          user,
		ctx:           ctx,
		cancel:        cancel,
	}

	host := s.user
	if host == "" {
		host = "unknown"
	}
	prompt := host + "> "

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     "/tmp/dolphin_history",
		InterruptPrompt: "^C",
	})
	if err == nil {
		s.rl = rl
	}
	return s
}

func (s *Stdio) ID() string { return s.id }

func (s *Stdio) Context() string          { return "" }
func (s *Stdio) Tools() []common.ToolDesc { return nil }

func (s *Stdio) Read(ctx context.Context) (string, error) {
	if s.rl != nil {
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)

		go func() {
			for {
				line, err := s.rl.Readline()
				if err != nil {
					errCh <- err
					return
				}
				if line != "" {
					lineCh <- s.maybeExit(line)
					return
				}
				// empty line, keep waiting
			}
		}()

		select {
		case line := <-lineCh:
			return line, nil
		case err := <-errCh:
			return "", err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// Fallback: read from buffered stdin.
	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		for {
			line, err := s.reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			line = line[:len(line)-1] // trim trailing newline
			if line != "" {
				lineCh <- s.maybeExit(line)
				return
			}
			// empty line, keep waiting
		}
	}()

	select {
	case line := <-lineCh:
		return line, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *Stdio) maybeExit(line string) string {
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "exit", "quit", "/exit", "/quit":
		fmt.Fprintln(os.Stdout, "bye")
		os.Exit(0)
	}
	return line
}

func (s *Stdio) Write(ctx context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := fmt.Fprint(os.Stdout, text)
	return err
}

func (s *Stdio) Flush() error {
	_, err := fmt.Fprint(os.Stdout, "\n")
	return err
}

func (s *Stdio) Close() error {
	s.cancel()
	if s.rl != nil {
		return s.rl.Close()
	}
	return nil
}

func (s *Stdio) Capability() Capability {
	return Capability{
		Interactive:        true,
		Streamable:         true,
		NestRead:           true,
		RenderTextMarkdown: "none",
	}
}

// Ensure Stdio implements IO.
var _ IO = (*Stdio)(nil)

// NullTransport is a no-op transport for testing.
type NullTransport struct {
	*SessionHolder
	id      string
	readBuf []string
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewNullTransport(id string) *NullTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &NullTransport{
		SessionHolder: NewSessionHolder(nil),
		id:            id,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (n *NullTransport) ID() string { return n.id }

func (n *NullTransport) Context() string          { return "" }
func (n *NullTransport) Tools() []common.ToolDesc { return nil }

func (n *NullTransport) Read(ctx context.Context) (string, error) {
	if len(n.readBuf) == 0 {
		return "", io.EOF
	}
	s := n.readBuf[0]
	n.readBuf = n.readBuf[1:]
	return s, nil
}

func (n *NullTransport) Write(ctx context.Context, text string) error { return nil }
func (n *NullTransport) Flush() error                                 { return nil }
func (n *NullTransport) Close() error                                 { n.cancel(); return nil }
func (n *NullTransport) Capability() Capability {
	return Capability{Interactive: false, Streamable: false, NestRead: false, RenderTextMarkdown: "none"}
}

var _ IO = (*NullTransport)(nil)
