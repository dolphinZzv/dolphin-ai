package agent

import (
	"testing"
)

func TestNewChannelIO(t *testing.T) {
	cio := NewChannelIO("test task")
	if cio == nil {
		t.Fatal("NewChannelIO returned nil")
	}
}

func TestChannelIOReadLine(t *testing.T) {
	cio := NewChannelIO("hello task")
	msg, err := cio.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine error: %v", err)
	}
	if msg != "hello task" {
		t.Errorf("ReadLine = %q", msg)
	}
}

func TestChannelIOWriteLine(t *testing.T) {
	cio := NewChannelIO("task")
	if err := cio.WriteLine("response"); err != nil {
		t.Errorf("WriteLine error: %v", err)
	}
}

func TestChannelIOWriteString(t *testing.T) {
	cio := NewChannelIO("task")
	if err := cio.WriteString("response"); err != nil {
		t.Errorf("WriteString error: %v", err)
	}
}
