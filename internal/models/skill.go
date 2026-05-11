package models

import "time"

type Skill struct {
	ID          uint      `gorm:"primaryKey;autoIncrement"`
	ProjectID   uint      `gorm:"not null;index"`
	Name        string    `gorm:"type:varchar(255);not null"`
	Description string    `gorm:"type:text"`
	Definition  string    `gorm:"type:text;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime"`

	Project Project `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}
