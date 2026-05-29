package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// ToolHandler implements a single MCP tool.
type ToolHandler struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Handle      func(ctx context.Context, args json.RawMessage) (any, error)
}

// Server runs MCP protocol over stdin/stdout.
type Server struct {
	tools []ToolHandler
}

func NewServer(tools ...ToolHandler) *Server {
	return &Server{tools: tools}
}

func (s *Server) Serve(ctx context.Context) error {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}

		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		switch req.Method {
		case "tools/list":
			s.handleList(req.ID)
		case "tools/call":
			s.handleCall(ctx, req.ID, req.Params)
		}
	}
	return sc.Err()
}

func (s *Server) handleList(id int) {
	tools := make([]map[string]any, len(s.tools))
	for i, t := range s.tools {
		tools[i] = map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.Schema,
		}
	}
	s.writeResponse(id, map[string]any{"tools": tools})
}

func (s *Server) handleCall(ctx context.Context, id int, params json.RawMessage) {
	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		s.writeError(id, -32602, "Invalid params: "+err.Error())
		return
	}

	for _, t := range s.tools {
		if t.Name == req.Name {
			result, err := t.Handle(ctx, req.Arguments)
			if err != nil {
				s.writeError(id, -32000, err.Error())
				return
			}
			data, _ := json.Marshal(result)
			s.writeResponse(id, map[string]any{"content": string(data)})
			return
		}
	}

	s.writeError(id, -32601, "Tool not found: "+req.Name)
}

func (s *Server) writeResponse(id int, result map[string]any) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(data))
}

func (s *Server) writeError(id int, code int, message string) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(data))
}
