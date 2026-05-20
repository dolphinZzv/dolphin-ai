package models

import "time"

type IssueFilter struct {
	ProjectID   *uint
	State       []IssueState
	AssigneeIDs []uint
	LabelIDs    []uint
	MilestoneID *uint
	Priority    *Priority
	CreatorID   *uint
	Search      string
	Limit       int
	Offset      int
	OrderBy     string
	OrderDir    string
}

type AgentFilter struct {
	Kind         *AgentKind
	Status       *AgentStatus
	Capabilities []CapabilityType
	ProjectID    *uint
	Limit        int
	Offset       int
}

type FeedbackFilter struct {
	TargetType *FeedbackTargetType
	TargetID   *uint
	AuthorID   *uint
	Limit      int
	Offset     int
}

type ProposalFilter struct {
	ProjectID *uint
	State     []ProposalState
	Priority  *Priority
	AuthorID  *uint
	Search    string
	Limit     int
	Offset    int
	OrderBy   string
	OrderDir  string
}

type TaskFilter struct {
	ProposalID *uint
	State      []TaskState
	AssigneeID *uint
	Priority   *Priority
	Search     string
	Limit      int
	Offset     int
	OrderBy    string
	OrderDir   string
}

// TimelineEvent type constants
const (
	EventIssueCreated         = "issue_created"
	EventIssueStateChanged    = "state_changed"
	EventIssueAssigned        = "assigned"
	EventIssueUnassigned      = "unassigned"
	EventCommentAdded         = "comment_added"
	EventAssigneeStateChanged = "assignee_state_changed"
	EventFeedbackCreated      = "feedback_created"
	EventIssueTimedOut        = "issue_timeout"
	EventProposalCreated      = "proposal_created"
	EventProposalStateChanged = "proposal_state_changed"
	EventTaskCreated          = "task_created"
	EventTaskStateChanged     = "task_state_changed"
)

// UnixNullTime helps with nullable timestamps in SQLite
type UnixNullTime struct {
	Time  time.Time
	Valid bool
}
