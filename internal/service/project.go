package service

import (
	"fmt"

	"chick/internal/models"
	"chick/internal/repository"
)

type ProjectService struct {
	projectRepo repository.ProjectRepository
	memberRepo  repository.ProjectMemberRepository
}

func NewProjectService(projectRepo repository.ProjectRepository, memberRepo repository.ProjectMemberRepository) *ProjectService {
	return &ProjectService{projectRepo: projectRepo, memberRepo: memberRepo}
}

func (s *ProjectService) Create(name, description string) (*models.Project, error) {
	p := &models.Project{
		Name:        name,
		Description: description,
	}
	if err := s.projectRepo.Create(p); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

func (s *ProjectService) GetByID(id uint) (*models.Project, error) {
	return s.projectRepo.GetByID(id)
}

func (s *ProjectService) List() ([]models.Project, error) {
	return s.projectRepo.List()
}

func (s *ProjectService) Delete(id uint) error {
	return s.projectRepo.Delete(id)
}

func (s *ProjectService) AddMember(projectID, agentID uint, role models.ProjectRole) (*models.ProjectMember, error) {
	m := &models.ProjectMember{
		ProjectID: projectID,
		AgentID:   agentID,
		Role:      role,
	}
	if err := s.memberRepo.Add(m); err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}
	return m, nil
}

func (s *ProjectService) RemoveMember(projectID, agentID uint) error {
	return s.memberRepo.Remove(projectID, agentID)
}

func (s *ProjectService) ListMembers(projectID uint) ([]models.ProjectMember, error) {
	return s.memberRepo.ListByProject(projectID)
}
