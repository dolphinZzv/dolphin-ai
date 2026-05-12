package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type AgentKind string

const (
	AgentKindAI     AgentKind = "ai"
	AgentKindHuman  AgentKind = "human"
	AgentKindHybrid AgentKind = "hybrid"
)

type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusBusy    AgentStatus = "busy"
	AgentStatusOffline AgentStatus = "offline"
	AgentStatusError   AgentStatus = "error"
)

type CapabilityType string

const (
	CapCodeReview  CapabilityType = "CODE_REVIEW"
	CapCoding      CapabilityType = "CODING"
	CapTesting     CapabilityType = "TESTING"
	CapDevOps      CapabilityType = "DEVOPS"
	CapDesign      CapabilityType = "DESIGN"
	CapDocumentation CapabilityType = "DOCUMENTATION"
	CapAnalysis    CapabilityType = "ANALYSIS"
	CapManagement  CapabilityType = "MANAGEMENT"
)

type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *StringSlice) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan StringSlice: %v", value)
	}
	return json.Unmarshal(bytes, s)
}

type JSONMap map[string]interface{}

func (m JSONMap) Value() (driver.Value, error) {
	return json.Marshal(m)
}

func (m *JSONMap) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan JSONMap: %v", value)
	}
	return json.Unmarshal(bytes, m)
}

type Agent struct {
	ID           uint         `gorm:"primaryKey;autoIncrement"`
	Number       uint         `gorm:"not null;default:0"`
	Name         string       `gorm:"type:varchar(255);not null"`
	Kind         AgentKind    `gorm:"type:varchar(20);not null"`
	Status       AgentStatus  `gorm:"type:varchar(20);not null;default:online"`
	ExternalID   string       `gorm:"type:varchar(255);uniqueIndex"`
	SecretHash   string       `gorm:"type:varchar(255);not null"`
	Token        string       `gorm:"type:varchar(255);uniqueIndex"`
	SystemPrompt string       `gorm:"type:text"`
	Capabilities StringSlice  `gorm:"type:jsonb;serializer:json"`
	Metadata     JSONMap      `gorm:"type:jsonb;serializer:json"`
	DeviceInfo   string       `gorm:"type:text"`
	ModelInfo    string       `gorm:"type:varchar(255)"`
		LastIP       string       `gorm:"type:varchar(45)"`
	LastSeenAt   *time.Time   `gorm:"index"`
	CreatedAt    time.Time    `gorm:"autoCreateTime"`
	UpdatedAt    time.Time    `gorm:"autoUpdateTime"`

	Memberships []ProjectMember `gorm:"foreignKey:AgentID"`
	CreatedIssues []Issue       `gorm:"foreignKey:CreatorID"`
	Comments      []Comment     `gorm:"foreignKey:AuthorID"`
	IssueAssignees []IssueAssignee `gorm:"foreignKey:AgentID"`
}
