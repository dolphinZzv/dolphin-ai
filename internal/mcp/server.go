package mcp

import (
	"encoding/json"
	"log"
)

// Server is the MCP protocol server
type Server struct {
	registry  *ToolRegistry
	handlers  *Handlers
	resources *Resources
	prompts   *Prompts
}

func NewServer(handlers *Handlers) *Server {
	s := &Server{
		registry:  NewToolRegistry(),
		handlers:  handlers,
		resources: NewResources(handlers.projectSvc, handlers.agentSvc, handlers.issueSvc),
		prompts:   NewPrompts(),
	}
	handlers.RegisterAll(s.registry)
	return s
}

// HandleRequest routes a JSON-RPC request to the appropriate handler.
// agentID and remoteAddr are per-request authentication context (not global state).
func (s *Server) HandleRequest(req *Request, agentID uint, remoteAddr string) Response {
	if req.ID == nil {
		// Notification — no response expected
		return Response{JSONRPC: "2.0"}
	}

	switch req.Method {
	case MethodInitialize:
		return s.handleInitialize(req.ID, req.Params)
	case MethodPing:
		return NewResponse(req.ID, map[string]interface{}{"status": "ok"})
	case MethodToolsList:
		return s.handleToolsList(req.ID)
	case MethodToolsCall:
		return s.handleToolsCall(req.ID, req.Params, agentID, remoteAddr)
	case MethodResourcesList:
		return s.handleResourcesList(req.ID)
	case MethodResourcesRead:
		return s.handleResourcesRead(req.ID, req.Params)
	case MethodPromptsList:
		return s.handlePromptsList(req.ID)
	case MethodPromptsGet:
		return s.handlePromptsGet(req.ID, req.Params)
	default:
		return NewMethodNotFound(req.ID)
	}
}

func (s *Server) handleInitialize(id json.RawMessage, params json.RawMessage) Response {
	return NewResponse(id, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
			"prompts":   map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "chick",
			"version": "0.1.0",
		},
	})
}

func (s *Server) handleToolsList(id json.RawMessage) Response {
	return NewResponse(id, map[string]interface{}{
		"tools": s.registry.List(),
	})
}

func (s *Server) handleToolsCall(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return NewError(id, -32602, "Invalid tool call params: "+err.Error())
	}

	tool, ok := s.registry.Get(call.Name)
	if !ok || tool.Handler == nil {
		return NewError(id, -32602, "Unknown tool: "+call.Name)
	}

	log.Printf("[mcp] tool call: %s", call.Name)
	resp := tool.Handler(id, call.Arguments, agentID, remoteAddr)

	// Wrap result in standard MCP content blocks (spec 2024-11-05)
	if resp.Error == nil && resp.Result != nil {
		data, _ := json.Marshal(resp.Result)
		resp.Result = map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": string(data),
				},
			},
			"isError": false,
		}
	}

	return resp
}

func (s *Server) handleResourcesList(id json.RawMessage) Response {
	return NewResponse(id, map[string]interface{}{
		"resources": s.resources.List(),
	})
}

func (s *Server) handleResourcesRead(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	item, err := s.resources.Read(p.URI)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"contents": []interface{}{item},
	})
}

func (s *Server) handlePromptsList(id json.RawMessage) Response {
	return NewResponse(id, map[string]interface{}{
		"prompts": s.prompts.List(),
	})
}

func (s *Server) handlePromptsGet(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	content, err := s.prompts.Get(p.Name, p.Arguments)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"description": p.Name,
		"messages": []map[string]interface{}{
			{"role": "user", "content": map[string]interface{}{"type": "text", "text": content}},
		},
	})
}
