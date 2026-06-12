package panda

// AgentTimelineEntryType mirrors the Swift client's EntryType enum.
type AgentTimelineEntryType string

const (
	TimelineEntryThinking   AgentTimelineEntryType = "thinking"
	TimelineEntryToolCall   AgentTimelineEntryType = "toolCall"
	TimelineEntryToolResult AgentTimelineEntryType = "toolResult"
	TimelineEntryResponse   AgentTimelineEntryType = "response"
)

// AgentTimelineEntry mirrors the Swift client's Entry struct.
type AgentTimelineEntry struct {
	ID        string                 `json:"id"`
	Type      AgentTimelineEntryType `json:"type"`
	Content   string                 `json:"content"`
	ToolName  string                 `json:"toolName,omitempty"`
	ToolInput string                 `json:"toolInput,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// AgentTimelineBody mirrors the Swift client's AgentTimelineBody struct.
type AgentTimelineBody struct {
	Title       string               `json:"title,omitempty"`
	Entries     []AgentTimelineEntry `json:"entries"`
	Status      string               `json:"status"`
	ParentMsgID int64                `json:"parentMsgID"`
}
