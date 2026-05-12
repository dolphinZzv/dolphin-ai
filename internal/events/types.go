package events

// Payload types for typed event handling

type IssueCreatedPayload struct {
	IssueID   uint
	ProjectID uint
	CreatorID uint
	LabelIDs  []uint
}

type IssueStateChangedPayload struct {
	IssueID   uint
	ProjectID uint
	From      string
	To        string
	ActorID   uint
}

type CommentAddedPayload struct {
	CommentID uint
	IssueID   uint
	ProjectID uint
	AuthorID  uint
}

type IssueAssigneeChangedPayload struct {
	IssueID   uint
	ProjectID uint
	AgentID   uint
	Action    string
}

type AgentStatusChangedPayload struct {
	AgentID uint
	Status  string
}

type FeedbackCreatedPayload struct {
	FeedbackID uint
	TargetType string
	TargetID   uint
	AuthorID   uint
}
