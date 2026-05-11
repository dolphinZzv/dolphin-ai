package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"
	"time"

	"gorm.io/gorm"
)

type AgentRepo struct {
	db *gorm.DB
}

func NewAgentRepo(db *gorm.DB) repository.AgentRepository {
	return &AgentRepo{db: db}
}

func (r *AgentRepo) Create(agent *models.Agent) error {
	return r.db.Create(agent).Error
}

func (r *AgentRepo) GetByID(id uint) (*models.Agent, error) {
	var a models.Agent
	err := r.db.First(&a, id).Error
	return &a, err
}

func (r *AgentRepo) GetByExternalID(externalID string) (*models.Agent, error) {
	var a models.Agent
	err := r.db.Where("external_id = ?", externalID).First(&a).Error
	return &a, err
}

func (r *AgentRepo) UpdateStatus(id uint, status models.AgentStatus) error {
	return r.db.Model(&models.Agent{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"status": status,
		}).Error
}

func (r *AgentRepo) UpdateLastSeen(id uint) error {
	now := time.Now()
	return r.db.Model(&models.Agent{}).Where("id = ?", id).
		Update("last_seen_at", &now).Error
}

func (r *AgentRepo) FindByCapability(capability models.CapabilityType, projectID uint) ([]models.Agent, error) {
	var agents []models.Agent
	err := r.db.Joins("JOIN project_members ON project_members.agent_id = agents.id").
		Where("project_members.project_id = ?", projectID).
		Where("agents.status = ?", models.AgentStatusOnline).
		Where("agents.capabilities @> ?", `["`+string(capability)+`"]`).
		Find(&agents).Error
	return agents, err
}

func (r *AgentRepo) FindOnlineByProject(projectID uint) ([]models.Agent, error) {
	var agents []models.Agent
	err := r.db.Joins("JOIN project_members ON project_members.agent_id = agents.id").
		Where("project_members.project_id = ?", projectID).
		Where("agents.status = ?", models.AgentStatusOnline).
		Find(&agents).Error
	return agents, err
}

func (r *AgentRepo) FindTimedOut(cutoffTime time.Time) ([]models.Agent, error) {
	var agents []models.Agent
	err := r.db.Where("last_seen_at IS NOT NULL AND last_seen_at < ?", cutoffTime).
		Find(&agents).Error
	return agents, err
}

func (r *AgentRepo) List(filter models.AgentFilter) ([]models.Agent, error) {
	q := r.db.Model(&models.Agent{})
	if filter.Kind != nil {
		q = q.Where("kind = ?", *filter.Kind)
	}
	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.ProjectID != nil {
		q = q.Joins("JOIN project_members ON project_members.agent_id = agents.id").
			Where("project_members.project_id = ?", *filter.ProjectID)
	}
	if len(filter.Capabilities) > 0 {
		for _, cap := range filter.Capabilities {
			q = q.Where("capabilities @> ?", `["`+string(cap)+`"]`)
		}
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		q = q.Offset(filter.Offset)
	}
	var agents []models.Agent
	err := q.Find(&agents).Error
	return agents, err
}

func (r *AgentRepo) CountByKind(kind models.AgentKind) (int64, error) {
	var count int64
	err := r.db.Model(&models.Agent{}).Where("kind = ?", kind).Count(&count).Error
	return count, err
}
