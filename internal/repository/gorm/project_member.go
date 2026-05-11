package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type ProjectMemberRepo struct {
	db *gorm.DB
}

func NewProjectMemberRepo(db *gorm.DB) repository.ProjectMemberRepository {
	return &ProjectMemberRepo{db: db}
}

func (r *ProjectMemberRepo) Add(member *models.ProjectMember) error {
	return r.db.Create(member).Error
}

func (r *ProjectMemberRepo) UpdateRole(projectID, agentID uint, role models.ProjectRole) error {
	return r.db.Model(&models.ProjectMember{}).
		Where("project_id = ? AND agent_id = ?", projectID, agentID).
		Update("role", role).Error
}

func (r *ProjectMemberRepo) Remove(projectID, agentID uint) error {
	return r.db.Where("project_id = ? AND agent_id = ?", projectID, agentID).
		Delete(&models.ProjectMember{}).Error
}

func (r *ProjectMemberRepo) ListByProject(projectID uint) ([]models.ProjectMember, error) {
	var list []models.ProjectMember
	err := r.db.Where("project_id = ?", projectID).
		Preload("Agent").Find(&list).Error
	return list, err
}

func (r *ProjectMemberRepo) ListByAgent(agentID uint) ([]models.ProjectMember, error) {
	var list []models.ProjectMember
	err := r.db.Where("agent_id = ?", agentID).
		Preload("Project").Find(&list).Error
	return list, err
}
