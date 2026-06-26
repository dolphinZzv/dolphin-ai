package models

import (
	"time"
)

type AssigneeState string

const (
	AssigneeStatePending    AssigneeState = "pending"
	AssigneeStateInProgress AssigneeState = "in_progress"
	AssigneeStateCompleted  AssigneeState = "completed"
	AssigneeStateBlocked    AssigneeState = "blocked"
)

type IssueAssignee struct {
	ID          uint          `gorm:"primaryKey;autoIncrement"`
	IssueID     uint          `gorm:"not null;index:idx_issue_agent,unique"`
	AgentID     uint          `gorm:"not null;index:idx_issue_agent,unique"`
	State       AssigneeState `gorm:"type:varchar(20);not null;default:pending"`
	AssignedAt  time.Time     `gorm:"autoCreateTime"`
	CompletedAt *time.Time

	Issue Issue `gorm:"foreignKey:IssueID;constraint:OnDelete:CASCADE"`
	Agent Agent `gorm:"foreignKey:AgentID;constraint:OnDelete:CASCADE"`
}
