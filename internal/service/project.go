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

var defaultLabels = []struct {
	name  string
	color string
}{
	{"bug", "#d73a4a"},
	{"enhancement", "#0052cc"},
	{"feature", "#008672"},
	{"question", "#fbca04"},
	{"documentation", "#006b75"},
	{"duplicate", "#cfd3d7"},
	{"wontfix", "#cfd3d7"},
	{"help wanted", "#159818"},
}

func (s *ProjectService) Create(name, description string) (*models.Project, error) {
	p := &models.Project{
		Name:        name,
		Description: description,
	}
	if err := s.projectRepo.Create(p); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	for _, lb := range defaultLabels {
		label := &models.Label{
			ProjectID: p.ID,
			Name:      lb.name,
			Color:     lb.color,
		}
		if err := s.labelRepo.Create(label); err != nil {
			return nil, fmt.Errorf("create default label %q: %w", lb.name, err)
		}
	}
	milestone := &models.Milestone{
		ProjectID:   p.ID,
		Title:       "v0.0.1",
		Description: "初始版本",
		State:       models.MilestoneOpen,
	}
	if err := s.milestoneRepo.Create(milestone); err != nil {
		return nil, fmt.Errorf("create default milestone: %w", err)
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

// GetMemberRole returns the role of an agent in a project, or an error if not a member.
func (s *ProjectService) GetMemberRole(projectID, agentID uint) (models.ProjectRole, error) {
	return s.memberRepo.GetRole(projectID, agentID)
}

// ListByAgent returns all projects the given agent is a member of.
func (s *ProjectService) ListByAgent(agentID uint) ([]models.Project, error) {
	members, err := s.memberRepo.ListByAgent(agentID)
	if err != nil {
		return nil, err
	}
	projects := make([]models.Project, 0, len(members))
	for _, m := range members {
		projects = append(projects, m.Project)
	}
	return projects, nil
}

// ─── Labels ─────────────────────────────────────────────────────

func (s *ProjectService) CreateLabel(projectID uint, name, color, capability, group string) (*models.Label, error) {
	l := &models.Label{
		ProjectID:  projectID,
		Name:       name,
		Color:      color,
		Capability: models.CapabilityType(capability),
		Group:      group,
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
	return s.labelRepo.ListByProject(projectID, group)
}

func (s *ProjectService) DeleteLabel(id uint) error {
	return s.labelRepo.Delete(id)
}

func (s *ProjectService) GetLabelByID(id uint) (*models.Label, error) {
	return s.labelRepo.GetByID(id)
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
	return s.milestoneRepo.ListByProject(projectID, state)
}

func (s *ProjectService) DeleteMilestone(id uint) error {
	return s.milestoneRepo.Delete(id)
}

func (s *ProjectService) GetMilestoneByID(id uint) (*models.Milestone, error) {
	return s.milestoneRepo.GetByID(id)
}
