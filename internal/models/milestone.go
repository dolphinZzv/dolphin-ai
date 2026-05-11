package models

import "time"

type MilestoneState string

const (
	MilestoneOpen   MilestoneState = "open"
	MilestoneClosed MilestoneState = "closed"
)

type Milestone struct {
	ID          uint           `gorm:"primaryKey;autoIncrement"`
	ProjectID   uint           `gorm:"not null;index"`
	Title       string         `gorm:"type:varchar(255);not null"`
	Description string         `gorm:"type:text"`
	State       MilestoneState `gorm:"type:varchar(20);not null;default:open"`
	DueDate     *time.Time
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	Project Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
	Issues  []Issue `gorm:"foreignKey:MilestoneID"`
}
