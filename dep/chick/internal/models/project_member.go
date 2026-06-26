package models

import "time"

type ProjectRole string

const (
	ProjectRoleOwner      ProjectRole = "owner"
	ProjectRoleMaintainer ProjectRole = "maintainer"
	ProjectRoleMember     ProjectRole = "member"
	ProjectRoleObserver   ProjectRole = "observer"
)

type ProjectMember struct {
	ID        uint        `gorm:"primaryKey;autoIncrement"`
	ProjectID uint        `gorm:"not null;index"`
	AgentID   uint        `gorm:"not null;index"`
	Role      ProjectRole `gorm:"type:varchar(20);not null;default:member"`
	CreatedAt time.Time   `gorm:"autoCreateTime"`

	Project Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
	Agent   Agent   `gorm:"foreignKey:AgentID;constraint:OnDelete:CASCADE"`
}
