package transport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/chzyer/readline"
)

// StdioTransport provides stdio-based interactive I/O using readline.
type StdioTransport struct {
	rl *readline.Instance
}

func NewStdioTransport() *StdioTransport {
	// History file path
	home, _ := os.UserHomeDir()
	historyDir := filepath.Join(home, ".dolphin")
	historyFile := filepath.Join(historyDir, "history")
	os.MkdirAll(historyDir, 0700)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:              "> ",
		HistoryFile:         historyFile,
		AutoComplete:        completer,
		InterruptPrompt:     "^C",
		EOFPrompt:           "exit",
		HistorySearchFold:   true,
		FuncFilterInputRune: nil,
	})
	if err != nil {
		// Fallback: create readline without history/complete
		rl, _ = readline.New("> ")
	}

	return &StdioTransport{rl: rl}
}

// tab completer for commands
var completer = readline.NewPrefixCompleter(
	readline.PcItem("/exit"),
	readline.PcItem("/quit"),
	readline.PcItem("/help"),
)

func (t *StdioTransport) Name() string { return "stdio" }

func (t *StdioTransport) Context() string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf("Connected via terminal. OS: %s/%s, Shell: %s, Home: %s",
		runtime.GOOS, runtime.GOARCH, os.Getenv("SHELL"), home)
}

func (t *StdioTransport) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Flushable: false, ConfirmExit: true}
}

func (t *StdioTransport) Start(ctx context.Context) error {
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
	_, err := fmt.Print(s)
	return err
}

func (t *StdioTransport) WriteLine(s string) error {
	msgsSent.Inc()
	_, err := fmt.Println(s)
	return err
}

func (t *StdioTransport) Close() error {
	return t.rl.Close()
}
