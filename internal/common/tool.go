package common

// ToolDesc describes an MCP tool source for a transport.
type ToolDesc struct {
	Name        string
	Description string
	URL         string
	Command     string
	Args        []string
	Executor    any // tool.Executor — typed as any to avoid import cycle (common → tool → permission → transport → common)
}
