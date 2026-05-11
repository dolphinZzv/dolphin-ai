package models

import "time"

type Project struct {
	ID          uint      `gorm:"primaryKey;autoIncrement"`
	Name        string    `gorm:"type:varchar(255);not null"`
	Description string    `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	Members  []ProjectMember `gorm:"foreignKey:ProjectID"`
	Issues   []Issue         `gorm:"foreignKey:ProjectID"`
	Labels   []Label         `gorm:"foreignKey:ProjectID"`
	Milestones []Milestone   `gorm:"foreignKey:ProjectID"`
	Skills   []Skill         `gorm:"foreignKey:ProjectID"`
}
