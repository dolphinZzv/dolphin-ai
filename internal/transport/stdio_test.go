package transport

import (
	"context"
	"testing"
)

func TestStdioTransportName(t *testing.T) {
	tp := &StdioTransport{}
	if n := tp.Name(); n != "stdio" {
		t.Errorf("Name() = %q", n)
	}
}

func TestStdioTransportStart(t *testing.T) {
	tp := &StdioTransport{}
	if err := tp.Start(context.Background()); err != nil {
		t.Errorf("Start() error: %v", err)
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
