package service

import (
	"crypto/rand"
	"encoding/hex"
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
	agentRepo              repository.AgentRepository
	eventBus               *events.Bus
	tokenGen               TokenGenerator
	allowHumanRegistration bool
}

func NewAgentService(agentRepo repository.AgentRepository, eventBus *events.Bus, tokenGen TokenGenerator, allowHumanRegistration bool) *AgentService {
	return &AgentService{agentRepo: agentRepo, eventBus: eventBus, tokenGen: tokenGen, allowHumanRegistration: allowHumanRegistration}
}

func (s *AgentService) Register(name string, kind models.AgentKind, externalID, secret string, capabilities []string, deviceInfo, modelInfo string) (*models.Agent, error) {
	if kind == models.AgentKindHuman && !s.allowHumanRegistration {
		return nil, fmt.Errorf("human registration is disabled by the server administrator")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash secret: %w", err)
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
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
		Token:        hex.EncodeToString(tokenBytes),
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
			Payload: events.AgentStatusChangedPayload{
				AgentID: id,
				Status:  string(status),
			},
		})
	}
	return nil
}

// Authenticate verifies an agent token and returns the agent.
func (s *AgentService) Authenticate(token string) (*models.Agent, error) {
	if token == "" {
		return nil, errors.New("empty token")
	}
	a, err := s.agentRepo.FindByToken(token)
	if err != nil {
		return nil, errors.New("invalid token")
	}
	return a, nil
}

func (s *AgentService) Heartbeat(id uint) error {
	return s.agentRepo.UpdateLastSeen(id)
}

func (s *AgentService) UpdateIP(id uint, ip string) error {
	return s.agentRepo.UpdateIP(id, ip)
}

func (s *AgentService) UpdateAllowedCIDRs(id uint, cidrs []string) error {
	if cidrs == nil {
		cidrs = []string{}
	}
	return s.agentRepo.UpdateAllowedCIDRs(id, cidrs)
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
