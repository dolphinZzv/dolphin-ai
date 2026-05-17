package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"dolphin/internal/config"

	"go.uber.org/zap"
)

// Transport abstracts the communication channel to an MCP server.
type Transport interface {
	Connect(ctx context.Context) error
	SendRequest(ctx context.Context, req map[string]any) (json.RawMessage, error)
	SendNotification(ctx context.Context, notif map[string]any) error
	Close() error
}

// stdioTransport communicates with a local MCP server subprocess via stdin/stdout.
type stdioTransport struct {
	cfg    config.MCPServerConfig
	name   string
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID atomic.Int64
}

// NewStdio creates a stdio-based transport for a local MCP server subprocess.
func NewStdio(name string, cfg config.MCPServerConfig) (*stdioTransport, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp server %q: command is required", name)
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mcp server %q: %w", name, err)
	}

	return &stdioTransport{
		cfg:    cfg,
		name:   name,
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdin),
		stdout: newLargeScanner(stdout),
	}, nil
}

func (t *stdioTransport) Connect(ctx context.Context) error {
	return nil
}

func (t *stdioTransport) SendRequest(ctx context.Context, req map[string]any) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.writeLine(req); err != nil {
		return nil, err
	}

	for t.stdout.Scan() {
		line := t.stdout.Text()
		if line == "" {
			continue
		}

		var msg struct {
			ID     int64           `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		reqID, _ := req["id"].(int64)
		if msg.ID != reqID {
			continue
		}

		if msg.Error != nil {
			return nil, fmt.Errorf("jsonrpc error: %s (code %d)", msg.Error.Message, msg.Error.Code)
		}
		return msg.Result, nil
	}

	if err := t.stdout.Err(); err != nil {
		return nil, fmt.Errorf("read error: %w", err)
	}
	return nil, fmt.Errorf("server closed connection")
}

func (t *stdioTransport) SendNotification(ctx context.Context, notif map[string]any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeLine(notif)
}

func (t *stdioTransport) Close() error {
	zap.S().Debugw("shutting down mcp server", "server", t.name)

	if err := t.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		zap.S().Debugw("signal not supported on this platform, killing mcp server", "server", t.name)
		return t.cmd.Process.Kill()
	}

	done := make(chan struct{})
	go func() {
		t.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		zap.S().Debugw("mcp server exited gracefully", "server", t.name)
		return nil
	case <-time.After(3 * time.Second):
		zap.S().Warnw("mcp server did not exit in time, killing", "server", t.name)
		return t.cmd.Process.Kill()
	}
}

func (t *stdioTransport) writeLine(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := t.stdin.Write(data); err != nil {
		return err
	}
	if _, err := t.stdin.Write([]byte("\n")); err != nil {
		return err
	}
	return t.stdin.Flush()
}

// newLargeScanner creates a bufio.Scanner with a 1MB buffer for large MCP responses.
func newLargeScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(bufio.NewReader(r))
	buf := make([]byte, 1024*1024)
	sc.Buffer(buf, 1024*1024)
	return sc
}
