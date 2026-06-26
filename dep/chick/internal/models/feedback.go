package models

import "time"

type FeedbackTargetType string

const (
	FeedbackTargetIssue     FeedbackTargetType = "issue"
	FeedbackTargetComment   FeedbackTargetType = "comment"
	FeedbackTargetAgent     FeedbackTargetType = "agent"
	FeedbackTargetAssignment FeedbackTargetType = "assignment"
)

type FeedbackDimension struct {
	Dimension string `json:"dimension"`
	Rating    int    `json:"rating"`
}

type Feedback struct {
	ID         uint               `gorm:"primaryKey;autoIncrement"`
	TargetType FeedbackTargetType `gorm:"type:varchar(20);not null"`
	TargetID   uint               `gorm:"not null;index:idx_feedback_target,unique"`
	AuthorID   uint               `gorm:"not null;index:idx_feedback_target,unique"`
	Rating     int                `gorm:"not null;check:rating >= 1 AND rating <= 5"`
	Dimensions JSONMap            `gorm:"type:jsonb;serializer:json"`
	Body       string             `gorm:"type:text"`
	CreatedAt  time.Time          `gorm:"autoCreateTime"`

	Author Agent `gorm:"foreignKey:AuthorID;constraint:OnDelete:CASCADE"`
}
