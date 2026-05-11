package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

// STDIOTransport reads JSON-RPC from stdin, writes to stdout
type STDIOTransport struct {
	reader *bufio.Reader
	writer io.Writer
}

func NewSTDIOTransport() *STDIOTransport {
	return &STDIOTransport{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

func (t *STDIOTransport) Read() (*Request, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	return &req, nil
}

func (t *STDIOTransport) Write(resp Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	data = append(data, '\n')
	_, err = t.writer.Write(data)
	return err
}

func (t *STDIOTransport) Run(handler func(*Request) Response) {
	log.Println("[mcp] STDIO transport starting")
	for {
		req, err := t.Read()
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("[mcp] read error: %v", err)
			return
		}
		resp := handler(req)
		if resp.ID != nil {
			if err := t.Write(resp); err != nil {
				log.Printf("[mcp] write error: %v", err)
				return
			}
		}
	}
}

// SSETransport handles SSE-based MCP connections over HTTP
// Keeps a single SSE connection alive for sending/receiving JSON-RPC messages
type SSETransport struct {
	sessions map[string]*SSESession
}

type SSESession struct {
	ID      string
	MsgCh   chan []byte
	Done    chan struct{}
	Handler func(*Request) Response
	Buffer  []Response
}

func NewSSETransport() *SSETransport {
	return &SSETransport{
		sessions: make(map[string]*SSESession),
	}
}
