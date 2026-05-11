package service

import (
	"fmt"

	"chick/internal/models"
	"chick/internal/repository"
)

type ProjectService struct {
	projectRepo   repository.ProjectRepository
	memberRepo    repository.ProjectMemberRepository
	labelRepo     repository.LabelRepository
	milestoneRepo repository.MilestoneRepository
}

func NewProjectService(
	projectRepo repository.ProjectRepository,
	memberRepo repository.ProjectMemberRepository,
	labelRepo repository.LabelRepository,
	milestoneRepo repository.MilestoneRepository,
) *ProjectService {
	return &ProjectService{
		projectRepo:   projectRepo,
		memberRepo:    memberRepo,
		labelRepo:     labelRepo,
		milestoneRepo: milestoneRepo,
	}
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

func (s *ProjectService) Update(id uint, name, description string) (*models.Project, error) {
	changes := map[string]interface{}{}
	if name != "" {
		changes["name"] = name
	}
	if description != "" {
		changes["description"] = description
	}
	if err := s.projectRepo.Update(id, changes); err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return s.projectRepo.GetByID(id)
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

func (s *ProjectService) UpdateMember(projectID, agentID uint, role models.ProjectRole) (*models.ProjectMember, error) {
	if err := s.memberRepo.UpdateRole(projectID, agentID, role); err != nil {
		return nil, fmt.Errorf("update member role: %w", err)
	}
	members, err := s.memberRepo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.AgentID == agentID {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("member not found")
}

func (s *ProjectService) RemoveMember(projectID, agentID uint) error {
	return s.memberRepo.Remove(projectID, agentID)
}

func (s *ProjectService) ListMembers(projectID uint) ([]models.ProjectMember, error) {
	return s.memberRepo.ListByProject(projectID)
}

// ─── Labels ─────────────────────────────────────────────────────

func (s *ProjectService) CreateLabel(projectID uint, name, color, capability, group string) (*models.Label, error) {
	l := &models.Label{
		ProjectID:   projectID,
		Name:        name,
		Color:       color,
		Capability:  models.CapabilityType(capability),
		Group:       group,
	}
	if l.Color == "" {
		l.Color = "#0366d6"
	}
	if err := s.labelRepo.Create(l); err != nil {
		return nil, fmt.Errorf("create label: %w", err)
	}
	return l, nil
}

func (s *ProjectService) ListLabels(projectID uint, group string) ([]models.Label, error) {
	labels, err := s.labelRepo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	if group == "" {
		return labels, nil
	}
	filtered := make([]models.Label, 0, len(labels))
	for _, l := range labels {
		if l.Group == group {
			filtered = append(filtered, l)
		}
	}
	return filtered, nil
}

func (s *ProjectService) DeleteLabel(id uint) error {
	return s.labelRepo.Delete(id)
}

// ─── Milestones ─────────────────────────────────────────────────

func (s *ProjectService) CreateMilestone(projectID uint, title, description string, dueDate *models.UnixNullTime) (*models.Milestone, error) {
	m := &models.Milestone{
		ProjectID:   projectID,
		Title:       title,
		Description: description,
		State:       models.MilestoneOpen,
	}
	if dueDate != nil && dueDate.Valid {
		m.DueDate = &dueDate.Time
	}
	if err := s.milestoneRepo.Create(m); err != nil {
		return nil, fmt.Errorf("create milestone: %w", err)
	}
	return m, nil
}

func (s *ProjectService) ListMilestones(projectID uint, state models.MilestoneState) ([]models.Milestone, error) {
	milestones, err := s.milestoneRepo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	if state == "" {
		return milestones, nil
	}
	filtered := make([]models.Milestone, 0, len(milestones))
	for _, m := range milestones {
		if m.State == state {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

func (s *ProjectService) DeleteMilestone(id uint) error {
	return s.milestoneRepo.Update(id, map[string]interface{}{"state": models.MilestoneClosed})
}
