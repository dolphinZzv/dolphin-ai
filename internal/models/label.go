package models

import "time"

type Label struct {
	ID          uint           `gorm:"primaryKey;autoIncrement"`
	ProjectID   uint           `gorm:"not null;index"`
	Name        string         `gorm:"type:varchar(100);not null"`
	Color       string         `gorm:"type:varchar(7);not null;default:#0366d6"`
	Description string         `gorm:"type:text"`
	Capability  CapabilityType `gorm:"type:varchar(30)"`
	Group       string         `gorm:"type:varchar(50)"`
	CreatedAt   time.Time      `gorm:"autoCreateTime"`

	Project Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
	Issues  []Issue `gorm:"many2many:issue_labels;"`
}
