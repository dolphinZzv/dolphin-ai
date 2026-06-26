package models

import "time"

type ProposalState string

const (
	ProposalStateDraft       ProposalState = "draft"
	ProposalStateSubmitted   ProposalState = "submitted"
	ProposalStateUnderReview ProposalState = "under_review"
	ProposalStateApproved    ProposalState = "approved"
	ProposalStateRejected    ProposalState = "rejected"
	ProposalStateInExecution ProposalState = "in_execution"
	ProposalStateCompleted   ProposalState = "completed"
	ProposalStateCancelled   ProposalState = "cancelled"
)

type Proposal struct {
	ID          uint          `gorm:"primaryKey;autoIncrement"`
	Number      uint          `gorm:"not null;uniqueIndex:idx_proposals_project_number"`
	ProjectID   uint          `gorm:"not null;uniqueIndex:idx_proposals_project_number"`
	Title       string        `gorm:"type:varchar(500);not null"`
	Description string        `gorm:"type:text"`
	State       ProposalState `gorm:"type:varchar(30);not null;default:draft"`
	Priority    Priority      `gorm:"type:varchar(20);not null;default:medium"`
	AuthorID    uint          `gorm:"not null;index"`
	ReviewerID  *uint         `gorm:"index"`
	ReviewNote  *string       `gorm:"type:text"`
	ReviewedAt  *time.Time
	SubmittedAt *time.Time
	ApprovedAt  *time.Time
	StartedAt   *time.Time `gorm:"index"`
	CompletedAt *time.Time `gorm:"index"`
	CancelledAt *time.Time
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	// Relations
	Project  Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
	Author   Agent   `gorm:"foreignKey:AuthorID;constraint:OnDelete:CASCADE"`
	Reviewer *Agent  `gorm:"foreignKey:ReviewerID"`
	Tasks    []Task  `gorm:"foreignKey:ProposalID"`
	Labels   []Label `gorm:"many2many:proposal_labels;"`
}
