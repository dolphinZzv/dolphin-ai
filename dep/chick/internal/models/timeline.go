package models

import "time"

type TimelineEvent struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	IssueID    *uint     `gorm:"index"`
	ProposalID *uint     `gorm:"index"`
	TaskID     *uint     `gorm:"index"`
	ActorID    uint      `gorm:"not null;index"`
	EventType  string    `gorm:"type:varchar(50);not null"`
	Payload    JSONMap   `gorm:"type:jsonb;serializer:json"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`

	Issue    *Issue    `gorm:"foreignKey:IssueID;constraint:OnDelete:CASCADE"`
	Proposal *Proposal `gorm:"foreignKey:ProposalID;constraint:OnDelete:CASCADE"`
	Task     *Task     `gorm:"foreignKey:TaskID;constraint:OnDelete:CASCADE"`
	Actor    Agent     `gorm:"foreignKey:ActorID;constraint:OnDelete:CASCADE"`
}
