package gorm

import (
	"crypto/rand"
	"encoding/hex"
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
	return r.db.Transaction(func(tx *gorm.DB) error {
		var max struct {
			Number uint
		}
		if err := tx.Model(&models.Agent{}).
			Select("COALESCE(MAX(number), 0) as number").
			Scan(&max).Error; err != nil {
			return err
		}
		agent.Number = max.Number + 1
		if agent.Token == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			agent.Token = hex.EncodeToString(b)
		}
		return tx.Create(agent).Error
	})
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

func (r *AgentRepo) FindByToken(token string) (*models.Agent, error) {
	var a models.Agent
	err := r.db.Where("token = ?", token).First(&a).Error
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

func (r *AgentRepo) UpdateIP(id uint, ip string) error {
	return r.db.Model(&models.Agent{}).Where("id = ?", id).
		Update("last_ip", ip).Error
}

func (r *AgentRepo) UpdateAllowedCIDRs(id uint, cidrs []string) error {
	return r.db.Model(&models.Agent{}).Where("id = ?", id).
		Update("allowed_cidrs", models.StringSlice(cidrs)).Error
}

func (r *AgentRepo) UpdateDisabled(id uint, disabled bool) error {
	return r.db.Model(&models.Agent{}).Where("id = ?", id).
		Update("disabled", disabled).Error
}

func (r *AgentRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Agent{}).Where("id = ?", id).Updates(changes).Error
}

func (r *AgentRepo) Delete(id uint) error {
	return r.db.Delete(&models.Agent{}, id).Error
}

func (r *AgentRepo) FindByCapability(capability models.CapabilityType, projectID uint) ([]models.Agent, error) {
	// First, get all online project members (capabilities filter is done in Go for cross-DB safety)
	var agents []models.Agent
	err := r.db.Joins("JOIN project_members ON project_members.agent_id = agents.id").
		Where("project_members.project_id = ?", projectID).
		Where("agents.status = ?", models.AgentStatusOnline).
		Find(&agents).Error
	if err != nil {
		return nil, err
	}
	// Filter in Go: check exact capability match in the JSON array
	filtered := make([]models.Agent, 0, len(agents))
	for _, a := range agents {
		for _, c := range a.Capabilities {
			if c == string(capability) {
				filtered = append(filtered, a)
				break
			}
		}
	}
	return filtered, nil
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
			q = q.Where("capabilities LIKE ?", `%"`+string(cap)+`"%`)
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

func (r *AgentRepo) NextNumber() (uint, error) {
	var max struct {
		Max uint
	}
	err := r.db.Model(&models.Agent{}).Select("COALESCE(MAX(number), 0) as max").Scan(&max).Error
	return max.Max + 1, err
}
