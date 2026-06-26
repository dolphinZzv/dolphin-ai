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

func (r *ProjectMemberRepo) CheckSharedProject(agentID1, agentID2 uint) (bool, error) {
	var count int64
	err := r.db.Model(&models.ProjectMember{}).
		Where("agent_id IN ? AND project_id IN (SELECT project_id FROM project_members WHERE agent_id = ?)", []uint{agentID1, agentID2}, agentID1).
		Group("project_id").
		Having("COUNT(DISTINCT agent_id) = 2").
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *ProjectMemberRepo) GetRole(projectID, agentID uint) (models.ProjectRole, error) {
	var member models.ProjectMember
	err := r.db.Where("project_id = ? AND agent_id = ?", projectID, agentID).
		First(&member).Error
	if err != nil {
		return "", err
	}
	return member.Role, nil
}
