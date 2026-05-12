package service

import (
	"errors"
	"fmt"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type TokenGenerator interface {
	GenerateToken(agentID uint) (string, error)
}

type AgentService struct {
	agentRepo repository.AgentRepository
	eventBus  *events.Bus
	tokenGen  TokenGenerator
}

func NewAgentService(agentRepo repository.AgentRepository, eventBus *events.Bus, tokenGen TokenGenerator) *AgentService {
	return &AgentService{agentRepo: agentRepo, eventBus: eventBus, tokenGen: tokenGen}
}

func (s *AgentService) Register(name string, kind models.AgentKind, externalID, secret string, capabilities []string, deviceInfo, modelInfo string) (*models.Agent, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash secret: %w", err)
	}

	caps := make(models.StringSlice, len(capabilities))
	for i, c := range capabilities {
		caps[i] = c
	}

	a := &models.Agent{
		Name:         name,
		Kind:         kind,
		Status:       models.AgentStatusOnline,
		ExternalID:   externalID,
		SecretHash:   string(hash),
		Capabilities: caps,
		DeviceInfo:   deviceInfo,
		ModelInfo:    modelInfo,
	}
	if err := s.agentRepo.Create(a); err != nil {
		return nil, fmt.Errorf("register agent: %w", err)
	}
	return a, nil
}

type LoginResult struct {
	Agent *models.Agent
	Token string
}

func (s *AgentService) Login(externalID, secret string) (*LoginResult, error) {
	a, err := s.agentRepo.GetByExternalID(externalID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid credentials")
		}
		return nil, fmt.Errorf("login: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(a.SecretHash), []byte(secret)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	token := ""
	if s.tokenGen != nil {
		token, err = s.tokenGen.GenerateToken(a.ID)
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
	}

	return &LoginResult{Agent: a, Token: token}, nil
}

func (s *AgentService) GetByID(id uint) (*models.Agent, error) {
	return s.agentRepo.GetByID(id)
}

func (s *AgentService) GetByExternalID(externalID string) (*models.Agent, error) {
	return s.agentRepo.GetByExternalID(externalID)
}

func (s *AgentService) UpdateStatus(id uint, status models.AgentStatus) error {
	if err := s.agentRepo.UpdateStatus(id, status); err != nil {
		return err
	}
	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventAgentStatusChanged,
			Payload: map[string]interface{}{
				"agentID": id,
				"status":  string(status),
			},
		})
	}
	return nil
}

func (s *AgentService) Heartbeat(id uint) error {
	return s.agentRepo.UpdateLastSeen(id)
}

func (s *AgentService) FindByCapability(capability models.CapabilityType, projectID uint) ([]models.Agent, error) {
	return s.agentRepo.FindByCapability(capability, projectID)
}

func (s *AgentService) List(filter models.AgentFilter) ([]models.Agent, error) {
	return s.agentRepo.List(filter)
}

func (s *AgentService) CountByKind(kind models.AgentKind) (int64, error) {
	return s.agentRepo.CountByKind(kind)
}
