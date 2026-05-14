package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	"chick/internal/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// SeedData populates the database with initial data if it's empty.
func SeedData(db *gorm.DB) error {
	var count int64
	if err := db.Model(&models.Agent{}).Count(&count).Error; err != nil {
		return fmt.Errorf("seed: count agents: %w", err)
	}
	if count > 0 {
		return nil
	}
	log.Println("[seed] database is empty, creating seed data...")

	// ── Admin agent ──────────────────────────────────────────
	hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	token := randomHex(32)
	admin := &models.Agent{
		Name:       "admin",
		Kind:       models.AgentKindHuman,
		Status:     models.AgentStatusOnline,
		ExternalID: "admin",
		SecretHash: string(hash),
		Token:      token,
	}
	if err := db.Create(admin).Error; err != nil {
		return fmt.Errorf("seed: create admin: %w", err)
	}
	log.Printf("[seed] created admin agent (externalID=admin, secret=admin, token=%s)", token)

	// ── Demo project ─────────────────────────────────────────
	project := &models.Project{
		Name:        "Demo 项目",
		Description: "Chick Agent 协作平台的示例项目，包含种子数据供体验。",
	}
	if err := db.Create(project).Error; err != nil {
		return fmt.Errorf("seed: create project: %w", err)
	}

	// Auto-increment for Project number
	_ = db.Model(&models.Project{}).Where("id = ?", project.ID).
		Update("number", 1)

	// Add admin as owner
	if err := db.Create(&models.ProjectMember{
		ProjectID: project.ID,
		AgentID:   admin.ID,
		Role:      models.ProjectRoleOwner,
	}).Error; err != nil {
		return fmt.Errorf("seed: add owner: %w", err)
	}

	// ── Default labels ───────────────────────────────────────
	defaultLabels := []models.Label{
		{ProjectID: project.ID, Name: "bug", Color: "#d73a4a", Description: "报告的问题"},
		{ProjectID: project.ID, Name: "enhancement", Color: "#0052cc", Description: "功能增强"},
		{ProjectID: project.ID, Name: "feature", Color: "#008672", Description: "新功能"},
		{ProjectID: project.ID, Name: "documentation", Color: "#006b75", Description: "文档"},
		{ProjectID: project.ID, Name: "question", Color: "#fbca04", Description: "问题咨询"},
		{ProjectID: project.ID, Name: "good first issue", Color: "#159818", Description: "适合新手"},
	}
	for i := range defaultLabels {
		if err := db.Create(&defaultLabels[i]).Error; err != nil {
			return fmt.Errorf("seed: create label %q: %w", defaultLabels[i].Name, err)
		}
	}

	// ── Default milestone ────────────────────────────────────
	milestone := &models.Milestone{
		ProjectID:   project.ID,
		Title:       "v0.1.0",
		Description: "初始版本",
		State:       models.MilestoneOpen,
	}
	if err := db.Create(milestone).Error; err != nil {
		return fmt.Errorf("seed: create milestone: %w", err)
	}

	// ── Demo issues ──────────────────────────────────────────
	demoIssues := []struct {
		title       string
		description string
		priority    models.Priority
		state       models.IssueState
		labelNames  []string
	}{
		{
			title:       "欢迎使用 Chick",
			description: "这是一个协作平台，支持 **Markdown** 格式。\n\n- 使用看板管理 Issue\n- 通过 MCP 协议接入 AI Agent\n- 实时通知和状态更新",
			priority:    models.PriorityMedium,
			state:       models.IssueStateOpen,
			labelNames:  []string{"feature", "documentation"},
		},
		{
			title:       "配置 MCP 客户端连接",
			description: "1. 创建 Agent 账户\n2. 获取 Token\n3. 使用 `claude mcp add` 或 OpenCode 配置连接\n\n详细文档请参考项目 Wiki。",
			priority:    models.PriorityHigh,
			state:       models.IssueStateOpen,
			labelNames:  []string{"documentation"},
		},
		{
			title:       "搭建开发环境",
			description: "## 环境要求\n\n- Go 1.22+\n- Node.js 20+\n- SQLite（开发环境）\n\n## 启动步骤\n\n```bash\nmake dev\n```",
			priority:    models.PriorityLow,
			state:       models.IssueStateInProgress,
			labelNames:  nil,
		},
		{
			title:       "设计系统图标和 Logo",
			description: "需要一套统一的图标风格和项目 Logo。",
			priority:    models.PriorityLow,
			state:       models.IssueStateLater,
			labelNames:  []string{"enhancement"},
		},
	}

	for _, d := range demoIssues {
		issue := &models.Issue{
			ProjectID:   project.ID,
			Title:       d.title,
			Description: d.description,
			Priority:    d.priority,
			State:       d.state,
			CreatorID:   admin.ID,
		}
		if err := db.Create(issue).Error; err != nil {
			return fmt.Errorf("seed: create issue %q: %w", d.title, err)
		}

		// Assign to admin as demo assignee
		_ = db.Create(&models.IssueAssignee{
			IssueID: issue.ID,
			AgentID: admin.ID,
			State:   models.AssigneeStatePending,
		})

		// Add labels
		for _, ln := range d.labelNames {
			for _, lbl := range defaultLabels {
				if lbl.Name == ln {
					db.Table("issue_labels").Create(map[string]interface{}{
						"issue_id": issue.ID,
						"label_id": lbl.ID,
					})
					break
				}
			}
		}
	}

	log.Println("[seed] seed data created successfully")
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("randomHex: " + err.Error())
	}
	return hex.EncodeToString(b)
}
