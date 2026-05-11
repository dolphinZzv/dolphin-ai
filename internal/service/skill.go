package service

import (
	"fmt"

	"chick/internal/models"
	"chick/internal/repository"
)

type SkillService struct {
	skillRepo repository.SkillRepository
}

func NewSkillService(skillRepo repository.SkillRepository) *SkillService {
	return &SkillService{skillRepo: skillRepo}
}

func (s *SkillService) ListByProject(projectID uint) ([]models.Skill, error) {
	return s.skillRepo.ListByProject(projectID)
}

func (s *SkillService) GetByID(id uint) (*models.Skill, error) {
	return s.skillRepo.GetByID(id)
}

func (s *SkillService) Create(projectID uint, name, description, definition string) (*models.Skill, error) {
	sk := &models.Skill{
		ProjectID:   projectID,
		Name:        name,
		Description: description,
		Definition:  definition,
	}
	if err := s.skillRepo.Create(sk); err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}
	return sk, nil
}
