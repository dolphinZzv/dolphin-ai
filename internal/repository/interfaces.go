package repository

import (
	"time"

	"chick/internal/models"

	"gorm.io/gorm"
)

// ─── Project ──────────────────────────────────────────────

type ProjectRepository interface {
	Create(project *models.Project) error
	GetByID(id uint) (*models.Project, error)
	Update(id uint, changes map[string]interface{}) error
	Delete(id uint) error
	List() ([]models.Project, error)
}

// ─── ProjectMember ─────────────────────────────────────────

type ProjectMemberRepository interface {
	Add(member *models.ProjectMember) error
	UpdateRole(projectID, agentID uint, role models.ProjectRole) error
	Remove(projectID, agentID uint) error
	ListByProject(projectID uint) ([]models.ProjectMember, error)
	ListByAgent(agentID uint) ([]models.ProjectMember, error)
	GetRole(projectID, agentID uint) (models.ProjectRole, error)
	CheckSharedProject(agentID1, agentID2 uint) (bool, error)
}

// ─── Agent ─────────────────────────────────────────────────

type AgentRepository interface {
	Create(agent *models.Agent) error
	GetByID(id uint) (*models.Agent, error)
	GetByExternalID(externalID string) (*models.Agent, error)
	UpdateStatus(id uint, status models.AgentStatus) error
	UpdateLastSeen(id uint) error
	FindByCapability(capability models.CapabilityType, projectID uint) ([]models.Agent, error)
	FindOnlineByProject(projectID uint) ([]models.Agent, error)
	FindTimedOut(cutoffTime time.Time) ([]models.Agent, error)
	List(filter models.AgentFilter) ([]models.Agent, error)
	CountByKind(kind models.AgentKind) (int64, error)
		NextNumber() (uint, error)
		UpdateIP(id uint, ip string) error
		FindByToken(token string) (*models.Agent, error)
		UpdateAllowedCIDRs(id uint, cidrs []string) error
		UpdateDisabled(id uint, disabled bool) error
		Update(id uint, changes map[string]interface{}) error
		Delete(id uint) error
	}

// ─── Issue ─────────────────────────────────────────────────

type IssueRepository interface {
	Create(issue *models.Issue) error
	GetByID(id uint) (*models.Issue, error)
	GetByNumber(projectID uint, number uint) (*models.Issue, error)
	List(filter models.IssueFilter) ([]models.Issue, int64, error)
	UpdateState(id uint, state models.IssueState) error
	Update(id uint, changes map[string]interface{}) error
	AddLabel(issueID, labelID uint) error
	RemoveLabel(issueID, labelID uint) error
	Delete(id uint) error
	NextNumber(projectID uint) (uint, error)
	Transaction(fn func(db *gorm.DB) error) error
}

// ─── IssueAssignee ─────────────────────────────────────────

type IssueAssigneeRepository interface {
	Create(assignee *models.IssueAssignee) error
	GetByIssueAndAgent(issueID, agentID uint) (*models.IssueAssignee, error)
	UpdateState(issueID, agentID uint, state models.AssigneeState) error
	ListByIssue(issueID uint) ([]models.IssueAssignee, error)
	ListByAgent(agentID uint) ([]models.IssueAssignee, error)
	Remove(issueID, agentID uint) error
}

// ─── Comment ───────────────────────────────────────────────

type CommentRepository interface {
	Create(comment *models.Comment) error
	GetByID(id uint) (*models.Comment, error)
	ListByIssue(issueID uint) ([]models.Comment, error)
	ListByParent(parentID uint) ([]models.Comment, error)
	Update(id uint, body string) error
	Delete(id uint) error
}

// ─── Label ─────────────────────────────────────────────────

type LabelRepository interface {
	Create(label *models.Label) error
	GetByID(id uint) (*models.Label, error)
	ListByProject(projectID uint, group string) ([]models.Label, error)
	Update(id uint, changes map[string]interface{}) error
	Delete(id uint) error
}

// ─── Milestone ─────────────────────────────────────────────

type MilestoneRepository interface {
	Create(milestone *models.Milestone) error
	GetByID(id uint) (*models.Milestone, error)
	ListByProject(projectID uint, state models.MilestoneState) ([]models.Milestone, error)
	Update(id uint, changes map[string]interface{}) error
	Delete(id uint) error
}

// ─── Timeline ──────────────────────────────────────────────

type TimelineRepository interface {
	Create(event *models.TimelineEvent) error
	ListByIssue(issueID uint) ([]models.TimelineEvent, error)
}

// ─── Feedback ──────────────────────────────────────────────

type FeedbackRepository interface {
	Create(feedback *models.Feedback) error
	ListByTarget(targetType models.FeedbackTargetType, targetID uint) ([]models.Feedback, error)
}

// ─── Aggregate ─────────────────────────────────────────────

type Repositories struct {
	Project        ProjectRepository
	ProjectMember  ProjectMemberRepository
	Agent          AgentRepository
	Issue          IssueRepository
	IssueAssignee  IssueAssigneeRepository
	Comment        CommentRepository
	Label          LabelRepository
	Milestone      MilestoneRepository
	Timeline       TimelineRepository
	Feedback       FeedbackRepository
	DB             *gorm.DB
}
