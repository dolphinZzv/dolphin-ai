package models

import (
	"time"
)

type IssueState string

const (
	IssueStateOpen                IssueState = "open"
	IssueStateInProgress          IssueState = "in_progress"
	IssueStateBlocked             IssueState = "blocked"
	IssueStateReview              IssueState = "review"
	IssueStateLater               IssueState = "later"
	IssueStateClosedCompleted     IssueState = "closed_completed"
	IssueStateClosedNotPlanned    IssueState = "closed_not_planned"
	IssueStateClosedRejected      IssueState = "closed_rejected"
	IssueStateReopen              IssueState = "reopen"
	IssueStatePendingConfirmation IssueState = "pending_confirmation"
)

type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
)

type Issue struct {
	ID              uint       `gorm:"primaryKey;autoIncrement"`
	Number          uint       `gorm:"not null;uniqueIndex:idx_issues_project_number"`
	ProjectID       uint       `gorm:"not null;uniqueIndex:idx_issues_project_number"`
	Title           string     `gorm:"type:varchar(500);not null"`
	Description     string     `gorm:"type:text"`
	State           IssueState `gorm:"type:varchar(30);not null;default:open"`
	Priority        Priority   `gorm:"type:varchar(20);not null;default:medium"`
	CreatorID       uint       `gorm:"not null;index"`
	ParentID        *uint      `gorm:"index"`
	MilestoneID     *uint      `gorm:"index"`
	DueDate         *time.Time
	Environment     *string   `gorm:"type:varchar(100)"`
	Branch          *string   `gorm:"type:varchar(255)"`
	Link            *string   `gorm:"type:text"`
	StructuredOutput JSONMap  `gorm:"type:jsonb;serializer:json"`
	ClosedAt        *time.Time
	StartedAt       *time.Time `gorm:"type:timestamptz;index"`
	CompletedAt     *time.Time `gorm:"type:timestamptz;index"`
	Difficulty      *int       `gorm:"type:smallint"`
	CreatedAt       time.Time  `gorm:"autoCreateTime"`
	UpdatedAt       time.Time  `gorm:"autoUpdateTime"`

	Project   Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
	Creator   Agent   `gorm:"foreignKey:CreatorID;constraint:OnDelete:CASCADE"`
	Parent    *Issue  `gorm:"foreignKey:ParentID"`
	Milestone *Milestone `gorm:"foreignKey:MilestoneID"`

	Children        []Issue         `gorm:"foreignKey:ParentID"`
	Comments        []Comment       `gorm:"foreignKey:IssueID"`
	Assignees       []IssueAssignee `gorm:"foreignKey:IssueID"`
	TimelineEvents  []TimelineEvent `gorm:"foreignKey:IssueID"`
	Labels          []Label         `gorm:"many2many:issue_labels;"`
}
