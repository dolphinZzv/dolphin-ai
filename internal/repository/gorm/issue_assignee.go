package gorm

import (
	"time"

	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type IssueAssigneeRepo struct {
	db *gorm.DB
}

func NewIssueAssigneeRepo(db *gorm.DB) repository.IssueAssigneeRepository {
	return &IssueAssigneeRepo{db: db}
}

func (r *IssueAssigneeRepo) Create(assignee *models.IssueAssignee) error {
	return r.db.Create(assignee).Error
}

func (r *IssueAssigneeRepo) UpdateState(issueID, agentID uint, state models.AssigneeState) error {
	updates := map[string]interface{}{"state": state}
	if state == models.AssigneeStateCompleted {
		now := time.Now()
		updates["completed_at"] = &now
	}
	return r.db.Model(&models.IssueAssignee{}).
		Where("issue_id = ? AND agent_id = ?", issueID, agentID).
		Updates(updates).Error
}

func (r *IssueAssigneeRepo) ListByIssue(issueID uint) ([]models.IssueAssignee, error) {
	var list []models.IssueAssignee
	err := r.db.Where("issue_id = ?", issueID).
		Preload("Agent").Find(&list).Error
	return list, err
}

func (r *IssueAssigneeRepo) ListByAgent(agentID uint) ([]models.IssueAssignee, error) {
	var list []models.IssueAssignee
	err := r.db.Where("agent_id = ?", agentID).
		Preload("Issue").Find(&list).Error
	return list, err
}

func (r *IssueAssigneeRepo) Remove(issueID, agentID uint) error {
	return r.db.Where("issue_id = ? AND agent_id = ?", issueID, agentID).
		Delete(&models.IssueAssignee{}).Error
}
