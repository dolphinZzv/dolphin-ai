package models

import (
	"time"
)

type CommentContentType string

const (
	CommentMarkdown   CommentContentType = "markdown"
	CommentToolCall   CommentContentType = "tool_call"
	CommentToolResult CommentContentType = "tool_result"
	CommentCodeDiff   CommentContentType = "code_diff"
	CommentDecision   CommentContentType = "decision"
	CommentApproval   CommentContentType = "approval"
	CommentRejection  CommentContentType = "rejection"
	CommentStructured CommentContentType = "structured"
)

type Comment struct {
	ID              uint               `gorm:"primaryKey;autoIncrement"`
	IssueID         uint               `gorm:"not null;index"`
	AuthorID        uint               `gorm:"not null;index"`
	ParentID        *uint              `gorm:"index"`
	Body            string             `gorm:"type:text;not null"`
	ContentType     CommentContentType `gorm:"type:varchar(30);not null;default:markdown"`
	ToolCallData    JSONMap            `gorm:"type:jsonb;serializer:json"`
	StructuredData  JSONMap            `gorm:"type:jsonb;serializer:json"`
	Metadata        JSONMap            `gorm:"type:jsonb;serializer:json"`
	CreatedAt       time.Time          `gorm:"autoCreateTime"`
	UpdatedAt       time.Time          `gorm:"autoUpdateTime"`

	Issue  Issue    `gorm:"foreignKey:IssueID;constraint:OnDelete:CASCADE"`
	Author Agent    `gorm:"foreignKey:AuthorID;constraint:OnDelete:CASCADE"`
	Parent *Comment `gorm:"foreignKey:ParentID"`
	Replies []Comment `gorm:"foreignKey:ParentID"`
}
