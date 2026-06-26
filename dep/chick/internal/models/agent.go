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
	CapCodeReview    CapabilityType = "CODE_REVIEW"
	CapCoding        CapabilityType = "CODING"
	CapTesting       CapabilityType = "TESTING"
	CapDevOps        CapabilityType = "DEVOPS"
	CapDesign        CapabilityType = "DESIGN"
	CapDocumentation CapabilityType = "DOCUMENTATION"
	CapAnalysis      CapabilityType = "ANALYSIS"
	CapManagement    CapabilityType = "MANAGEMENT"
)

// SupportedModels is a predefined list of AI models for agent selection.
var SupportedModels = []string{
	"Claude 4 Opus",
	"Claude 4 Sonnet",
	"Claude 4 Haiku",
	"Claude 3.5 Sonnet",
	"Claude 3.5 Haiku",
	"GPT-4o",
	"GPT-4o mini",
	"GPT-4.1",
	"GPT-4.1 mini",
	"GPT-4.1 nano",
	"Gemini 2.5 Pro",
	"Gemini 2.5 Flash",
	"DeepSeek-V3",
	"DeepSeek-R1",
	"Qwen-Max",
	"Qwen-Plus",
	"Mistral Large",
	"自定义模型",
}

// CommonDeviceInfo is a predefined list of common device/OS info for suggestions.
var CommonDeviceInfo = []string{
	"VS Code",
	"Claude Code (CLI)",
	"Cursor",
	"Windsurf",
	"OpenCode (CLI)",
	"JetBrains IDE",
	"Linux / Chrome",
	"macOS / Chrome",
	"Windows / Chrome",
	"Linux / Firefox",
	"macOS / Safari",
	"API Direct",
}

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
	ID           uint        `gorm:"primaryKey;autoIncrement"`
	Number       uint        `gorm:"not null;default:0"`
	Name         string      `gorm:"type:varchar(255);not null"`
	Kind         AgentKind   `gorm:"type:varchar(20);not null"`
	Status       AgentStatus `gorm:"type:varchar(20);not null;default:online"`
	ExternalID   string      `gorm:"type:varchar(255);uniqueIndex"`
	SecretHash   string      `gorm:"type:varchar(255);not null"`
	Token        string      `gorm:"type:varchar(255);uniqueIndex"`
	SystemPrompt string      `gorm:"type:text"`
	Capabilities StringSlice `gorm:"type:jsonb;serializer:json"`
	Metadata     JSONMap     `gorm:"type:jsonb;serializer:json"`
	DeviceInfo   string      `gorm:"type:text"`
	ModelInfo    string      `gorm:"type:varchar(255)"`
	Disabled     bool        `gorm:"not null;default:false"`
	LastIP       string      `gorm:"type:varchar(45)"`
	AllowedCIDRs StringSlice `gorm:"type:jsonb;serializer:json"`
	LastSeenAt   *time.Time  `gorm:"index"`
	CreatedAt    time.Time   `gorm:"autoCreateTime"`
	UpdatedAt    time.Time   `gorm:"autoUpdateTime"`

	Memberships    []ProjectMember `gorm:"foreignKey:AgentID"`
	CreatedIssues  []Issue         `gorm:"foreignKey:CreatorID"`
	Comments       []Comment       `gorm:"foreignKey:AuthorID"`
	IssueAssignees []IssueAssignee `gorm:"foreignKey:AgentID"`
}
