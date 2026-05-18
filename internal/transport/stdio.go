package transport

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"dolphin/internal/config"
	"github.com/charmbracelet/glamour"

	"github.com/chzyer/readline"
)

const defaultPrompt = "Dolphin > "

// StdioTransport provides stdio-based interactive I/O using readline.
type StdioTransport struct {
	rl     *readline.Instance
	md     *glamour.TermRenderer // markdown renderer, nil when disabled
	mdBuf  strings.Builder        // buffer for accumulating streaming content
	rawOut bool                    // when true, WriteString bypasses markdown buffer
}

func NewStdioTransport(cfg *config.Config) *StdioTransport {
	// History file path
	home, _ := os.UserHomeDir()
	historyDir := filepath.Join(home, ".dolphin")
	historyFile := filepath.Join(historyDir, "history")
	os.MkdirAll(historyDir, 0700)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:              defaultPrompt,
		HistoryFile:         historyFile,
		AutoComplete:        completer,
		InterruptPrompt:     "^C",
		EOFPrompt:           "exit",
		HistorySearchFold:   true,
		FuncFilterInputRune: nil,
	})
	if err != nil {
		// Fallback: create readline without history/complete
		var fallbackErr error
		rl, fallbackErr = readline.New(defaultPrompt)
		if fallbackErr != nil {
			fmt.Fprintf(os.Stderr, "[stdio] readline fallback also failed: %v\n", fallbackErr)
		}
	}

	t := &StdioTransport{rl: rl}
	if cfg != nil && cfg.Transport.Stdio.MarkdownRender {
		t.md = newMarkdownRenderer(cfg.Transport.Stdio.MarkdownStyle)
	}
	return t
}

// tab completer for commands
var completer = readline.NewPrefixCompleter(
	readline.PcItem("/exit"),
	readline.PcItem("/quit"),
	readline.PcItem("/help"),
)

func (t *StdioTransport) Name() string { return "stdio" }

func shellName() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "windows" {
		for _, s := range []string{"pwsh.exe", "powershell.exe", "cmd.exe", "bash.exe"} {
			if _, err := exec.LookPath(s); err == nil {
				return s
			}
		}
	}
	return "unknown"
}

func (t *StdioTransport) Context() string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf("Connected via terminal. OS: %s/%s, Shell: %s, Home: %s",
		runtime.GOOS, runtime.GOARCH, shellName(), home)
}

func (t *StdioTransport) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Flushable: false, ConfirmExit: true, ShowToolDetails: true}
}

func (t *StdioTransport) Start(ctx context.Context) error {
	activeConnections.Add(1)
	return nil
}

func (t *StdioTransport) ReadLine() (string, error) {
	line, err := t.rl.Readline()
	if err == nil {
		msgsReceived.Inc()
	}
	return line, err
}

func (t *StdioTransport) WriteString(s string) error {
	msgsSent.Inc()
	if t.md != nil {
		t.mdBuf.WriteString(s)
		// Render and flush complete paragraphs (separated by blank lines)
		content := t.mdBuf.String()
		if idx := strings.LastIndex(content, "\n\n"); idx >= 0 {
			block := content[:idx+2]
			t.mdBuf.Reset()
			t.mdBuf.WriteString(content[idx+2:])
			rendered, err := t.md.Render(block)
			if err == nil {
				_, err = fmt.Print(rendered)
				return err
			}
		}
		return nil
	}
	_, err := fmt.Print(s)
	return err
}

func (t *StdioTransport) WriteLine(s string) error {
	msgsSent.Inc()
	// If markdown is enabled and we have buffered content, flush it rendered
	if t.md != nil && t.mdBuf.Len() > 0 {
		t.mdBuf.WriteString("\n")
		rendered, err := t.md.Render(t.mdBuf.String())
		t.mdBuf.Reset()
		if err == nil {
			fmt.Print(rendered)
			return nil
		}
	}
	_, err := fmt.Println(s)
	return err
}

func (t *StdioTransport) Close() error {
	activeConnections.Add(-1)
	if t.rl != nil {
		return t.rl.Close()
	}
	return nil
}
