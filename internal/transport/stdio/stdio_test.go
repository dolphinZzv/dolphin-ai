package stdio

import (
	"context"
	transport "dolphin/internal/transport"
	"testing"
)

func TestStdioTransportName(t *testing.T) {
	tp := &StdioTransport{}
	if n := tp.Name(); n != "stdio" {
		t.Errorf("Name() = %q", n)
	}
}

func TestStdioTransportStart(t *testing.T) {
	before := transport.ActiveConnections.Value()
	tp := &StdioTransport{}
	if err := tp.Start(context.Background()); err != nil {
		t.Errorf("Start() error: %v", err)
	}
	if got := transport.ActiveConnections.Value(); got != before+1 {
		t.Errorf("after Start, activeConnections = %d, want %d", got, before+1)
	}
	if err := tp.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	if got := transport.ActiveConnections.Value(); got != before {
		t.Errorf("after Close, activeConnections = %d, want %d", got, before)
	}
}

func TestStdioTransportWriteString(t *testing.T) {
	tp := &StdioTransport{}
	if err := tp.WriteString("test"); err != nil {
		t.Errorf("WriteString() error: %v", err)
	}
}

func TestStdioTransportWriteLine(t *testing.T) {
	tp := &StdioTransport{}
	if err := tp.WriteLine("test"); err != nil {
		t.Errorf("WriteLine() error: %v", err)
	}
}
