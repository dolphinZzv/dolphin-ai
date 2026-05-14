package mcp

// ToolRegistry manages MCP tool definitions
type ToolRegistry struct {
	tools map[string]*ToolDefinition
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*ToolDefinition),
	}
}

func (r *ToolRegistry) Register(tool *ToolDefinition) {
	r.tools[tool.Name] = tool
}

func (r *ToolRegistry) Get(name string) (*ToolDefinition, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *ToolRegistry) List() []ToolDefinition {
	list := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		list = append(list, *t)
	}
	return list
}

// inputSchema returns a JSON Schema for tool input parameters
func StringParam(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "string",
		"description": description,
	}
}

func StringRequiredParam(description string) map[string]interface{} {
	return StringParam(description)
}

func NumberParam(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "number",
		"description": description,
	}
}

func ArrayParam(description, itemsType string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"description": description,
		"items":       map[string]interface{}{"type": itemsType},
	}
}

func ObjectSchema(properties map[string]interface{}, required []string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
