package dream

import (
	"context"
	"time"

	"dolphin/internal/session"
	"dolphin/internal/types"
)

// Action describes what kind of edit to perform.
type Action string

const (
	ActionImprove   Action = "improve"
	ActionMerge     Action = "merge"
	ActionDeprecate Action = "deprecate"
	ActionCreate    Action = "create"
	ActionSplit     Action = "split"
)

// SignalType categorises the evidence behind a potential edit.
type SignalType string

const (
	SignalCorrection   SignalType = "correction"
	SignalPreference   SignalType = "preference"
	SignalRepetition   SignalType = "repetition"
	SignalObsolescence SignalType = "obsolescence"
	SignalRefinement   SignalType = "refinement"
)

// EditSignal is a raw detection from Phase 1 before it is matched to a
// concrete brain file.
type EditSignal struct {
	Type        SignalType `json:"type"`
	Target      string     `json:"target,omitempty"`
	Description string     `json:"description"`
	Evidence    []string   `json:"evidence"`
	Confidence  float64    `json:"confidence"`
	FirstSeen   time.Time  `json:"first_seen"`
}

// EditProposal is the output of Phase 1 — a concrete suggestion that may or
// may not need LLM refinement in Phase 2.
type EditProposal struct {
	ID           string   `json:"id"`
	Target       string   `json:"target"`
	Action       Action   `json:"action"`
	Before       string   `json:"before"`
	Reason       string   `json:"reason"`
	Evidence     []string `json:"evidence"`
	Confidence   float64  `json:"confidence"`
	Impact       float64  `json:"impact"`
	NeedsLLM     bool     `json:"needs_llm"`
	Sources      []string `json:"sources,omitempty"`
	IsRhetorical bool     `json:"is_rhetorical"`
}

// Edit is the final output of Phase 2 — an EditProposal with LLM-written
// After content, ready to apply.
type Edit struct {
	ProposalID string `json:"proposal_id"`
	Action     Action `json:"action"`
	Target     string `json:"target"`
	After      string `json:"after"`
	Reasoning  string `json:"reasoning"`
}

// TeachableMoment captures a user correction followed by a behavioural change.
type TeachableMoment struct {
	SessionID      string `json:"session_id"`
	UserCorrection string `json:"user_correction"`
	WrongAction    string `json:"wrong_action"`
	CorrectAction  string `json:"correct_action"`
	IsPreference   bool   `json:"is_preference"`
}

// RepeatedPattern captures a tool-call pattern recurring across sessions.
type RepeatedPattern struct {
	ToolName string   `json:"tool_name"`
	Pattern  string   `json:"pattern"`
	Count    int      `json:"count"`
	Sessions []string `json:"sessions"`
}

// SimilarGroup captures user questions that are semantically similar across
// sessions.
type SimilarGroup struct {
	Keywords     []string  `json:"keywords"`
	Questions    []string  `json:"questions"`
	Sessions     []string  `json:"sessions"`
	FirstAsked   time.Time `json:"first_asked"`
	LastAsked    time.Time `json:"last_asked"`
	RepeatedDays int       `json:"repeated_days"`
}

// ExtractResult is the Phase 1 output — structured observations from scanning
// recent sessions.
type ExtractResult struct {
	Sessions            int               `json:"sessions"`
	TotalRounds         int               `json:"total_rounds"`
	TotalMessages       int               `json:"total_messages"`
	ExplicitFacts       []string          `json:"explicit_facts"`
	ExplicitCommands    []string          `json:"explicit_commands"`
	TeachableMoments    []TeachableMoment `json:"teachable_moments"`
	RepeatedCommands    []RepeatedPattern `json:"repeated_commands"`
	ToolErrors          []ToolError       `json:"tool_errors"`
	PermissionDenials   []string          `json:"permission_denials"`
	SimilarQuestions    []SimilarGroup    `json:"similar_questions"`
	CompactionSummaries []string          `json:"compaction_summaries"`
}

// ToolError captures a failed tool execution that might indicate a systemic
// problem worth fixing in a brain file.
type ToolError struct {
	ToolName  string `json:"tool_name"`
	ErrorMsg  string `json:"error_msg"`
	SessionID string `json:"session_id"`
	Count     int    `json:"count"`
}

// BrainFile is a lightweight view of a brain .md file used during Phase 1.
type BrainFile struct {
	Path            string    `json:"path"`
	ReferencedCount int       `json:"referenced_count"`
	LastReferenced  time.Time `json:"last_referenced"`
	Size            int       `json:"size"`
}

// BrainContext is a compact summary of what is currently in the brain,
// passed to the Phase 2 LLM so it can avoid duplicate suggestions.
type BrainContext struct {
	Commands []string `json:"commands"`
	Facts    []string `json:"facts"`
	Profile  string   `json:"profile"`
}

// brainAccess is the subset of *brain.Brain methods used by Dream.
// Abstracted so tests can use a mock.
type brainAccess interface {
	Dir() string
	Read(ctx context.Context, path string) (string, error)
	List(ctx context.Context) ([]string, error)
	AutoCommit(ctx context.Context, msg string)
	Tree() (string, error)
}

// agentIOChecker is the subset of *agentio.AgentIO used by Dream.
type agentIOChecker interface {
	Processing() bool
}

// sessionLister is the subset of *session.Manager used by Dream.
type sessionLister interface {
	List(ctx context.Context) ([]*session.Session, error)
}

// sessionSnapshot is a read-once snapshot of a session's message history,
// immune to concurrent Compaction writes.
type sessionSnapshot struct {
	ID       string
	Messages []types.Message
}
