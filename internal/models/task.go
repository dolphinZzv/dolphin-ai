package models

import "time"

type TaskState string

const (
	TaskStatePending    TaskState = "pending"
	TaskStateInProgress TaskState = "in_progress"
	TaskStateCompleted  TaskState = "completed"
	TaskStateBlocked    TaskState = "blocked"
	TaskStateCancelled  TaskState = "cancelled"
)

type Task struct {
	ID          uint      `gorm:"primaryKey;autoIncrement"`
	Number      uint      `gorm:"not null;uniqueIndex:idx_tasks_proposal_number"`
	ProposalID  uint      `gorm:"not null;uniqueIndex:idx_tasks_proposal_number;index"`
	Title       string    `gorm:"type:varchar(500);not null"`
	Description string    `gorm:"type:text"`
	State       TaskState `gorm:"type:varchar(30);not null;default:pending"`
	Priority    Priority  `gorm:"type:varchar(20);not null;default:medium"`
	AssigneeID  *uint     `gorm:"index"`
	StartedAt   *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	// Relations
	Proposal Proposal `gorm:"foreignKey:ProposalID;constraint:OnDelete:CASCADE"`
	Assignee *Agent   `gorm:"foreignKey:AssigneeID"`
	Issues   []Issue  `gorm:"many2many:task_issues;"`
}
