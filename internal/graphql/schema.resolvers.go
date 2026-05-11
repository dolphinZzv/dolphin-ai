package graph

import (
	"context"
	"fmt"

	"chick/internal/models"
)

// mutationResolver implements MutationResolver.
type mutationResolver struct{ *Resolver }

// queryResolver implements QueryResolver.
type queryResolver struct{ *Resolver }

func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }
func (r *Resolver) Query() QueryResolver       { return &queryResolver{r} }

// ---- Agent Mutations ----

func (r *mutationResolver) RegisterAgent(ctx context.Context, name string, kind AgentKind, externalID string, secret string, capabilities []string) (*Agent, error) {
	caps := capabilities
	if caps == nil {
		caps = []string{}
	}
	a, err := r.AgentSvc.Register(name, models.AgentKind(kind), externalID, secret, caps)
	if err != nil {
		return nil, fmt.Errorf("register agent: %w", err)
	}
	return agentFromModel(a), nil
}

func (r *mutationResolver) LoginAgent(ctx context.Context, externalID string, secret string) (*LoginResult, error) {
	result, err := r.AgentSvc.Login(externalID, secret)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	return &LoginResult{
		Agent: agentFromModel(result.Agent),
		Token: result.Token,
	}, nil
}

// ---- Project Mutations ----

func (r *mutationResolver) CreateProject(ctx context.Context, name string, description *string) (*Project, error) {
	desc := ""
	if description != nil {
		desc = *description
	}
	p, err := r.ProjectSvc.Create(name, desc)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return projectFromModel(p), nil
}

func (r *mutationResolver) AddProjectMember(ctx context.Context, projectID string, agentID string, role ProjectRole) (*ProjectMember, error) {
	m, err := r.ProjectSvc.AddMember(parseID(projectID), parseID(agentID), models.ProjectRole(role))
	if err != nil {
		return nil, fmt.Errorf("add project member: %w", err)
	}
	return projectMemberFromModel(m), nil
}

func (r *mutationResolver) RemoveProjectMember(ctx context.Context, projectID string, agentID string) (bool, error) {
	if err := r.ProjectSvc.RemoveMember(parseID(projectID), parseID(agentID)); err != nil {
		return false, fmt.Errorf("remove project member: %w", err)
	}
	return true, nil
}

// ---- Issue Mutations ----

func (r *mutationResolver) CreateIssue(ctx context.Context, projectID string, creatorID string, title string, description *string, priority Priority, assigneeIDs []string, labelIDs []string) (*Issue, error) {
	desc := ""
	if description != nil {
		desc = *description
	}
	assigneeUintIDs := make([]uint, len(assigneeIDs))
	for i, id := range assigneeIDs {
		assigneeUintIDs[i] = parseID(id)
	}
	labelUintIDs := make([]uint, len(labelIDs))
	for i, id := range labelIDs {
		labelUintIDs[i] = parseID(id)
	}
	issue, err := r.IssueSvc.Create(parseID(projectID), parseID(creatorID), title, desc, models.Priority(priority), assigneeUintIDs, labelUintIDs)
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	return issueFromModel(issue), nil
}

func (r *mutationResolver) TransitionIssue(ctx context.Context, id string, newState IssueState, actorID string) (*Issue, error) {
	// Use WorkflowSvc to validate the transition
	issue, err := r.WorkflowSvc.Transition(parseID(id), models.IssueState(newState), parseID(actorID))
	if err != nil {
		return nil, fmt.Errorf("transition issue: %w", err)
	}
	return issueFromModel(issue), nil
}

func (r *mutationResolver) AddAssignee(ctx context.Context, issueID string, agentID string) (*IssueAssignee, error) {
	ia, err := r.IssueSvc.AddAssignee(parseID(issueID), parseID(agentID))
	if err != nil {
		return nil, fmt.Errorf("add assignee: %w", err)
	}
	return issueAssigneeFromModel(ia), nil
}

func (r *mutationResolver) RemoveAssignee(ctx context.Context, issueID string, agentID string) (bool, error) {
	if err := r.IssueSvc.RemoveAssignee(parseID(issueID), parseID(agentID)); err != nil {
		return false, fmt.Errorf("remove assignee: %w", err)
	}
	return true, nil
}

// ---- Comment Mutations ----

func (r *mutationResolver) AddComment(ctx context.Context, issueID string, authorID string, body string, contentType CommentContentType, parentID *string) (*Comment, error) {
	var pid *uint
	if parentID != nil {
		v := parseID(*parentID)
		pid = &v
	}
	c, err := r.CommentSvc.Create(parseID(issueID), parseID(authorID), body, models.CommentContentType(contentType), pid)
	if err != nil {
		return nil, fmt.Errorf("add comment: %w", err)
	}
	return commentFromModel(c), nil
}

// ---- Queries ----

func (r *queryResolver) Agents(ctx context.Context, kind *AgentKind, status *AgentStatus) ([]*Agent, error) {
	filter := models.AgentFilter{}
	if kind != nil {
		k := models.AgentKind(*kind)
		filter.Kind = &k
	}
	if status != nil {
		s := models.AgentStatus(*status)
		filter.Status = &s
	}
	agents, err := r.AgentSvc.List(filter)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	result := make([]*Agent, len(agents))
	for i, a := range agents {
		result[i] = agentFromModel(&a)
	}
	return result, nil
}

func (r *queryResolver) Projects(ctx context.Context) ([]*Project, error) {
	projects, err := r.ProjectSvc.List()
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	result := make([]*Project, len(projects))
	for i, p := range projects {
		result[i] = projectFromModel(&p)
	}
	return result, nil
}

func (r *queryResolver) Project(ctx context.Context, id string) (*Project, error) {
	p, err := r.ProjectSvc.GetByID(parseID(id))
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return projectFromModel(p), nil
}

func (r *queryResolver) Issue(ctx context.Context, id string) (*Issue, error) {
	issue, err := r.IssueSvc.GetByID(parseID(id))
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	return issueFromModel(issue), nil
}

func (r *queryResolver) Issues(ctx context.Context, projectID string, state *IssueState, priority *Priority, limit *int32, offset *int32) (*IssueConnection, error) {
	filter := models.IssueFilter{ProjectID: uintPtr(parseID(projectID))}
	if state != nil {
		filter.State = []models.IssueState{models.IssueState(*state)}
	}
	if priority != nil {
		p := models.Priority(*priority)
		filter.Priority = &p
	}
	if limit != nil {
		filter.Limit = int(*limit)
	}
	if offset != nil {
		filter.Offset = int(*offset)
	}

	issues, total, err := r.IssueSvc.List(filter)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	result := make([]*Issue, len(issues))
	for i, issue := range issues {
		result[i] = issueFromModel(&issue)
	}
	return &IssueConnection{
		Edges: result,
		Total: int32(total),
	}, nil
}

func (r *queryResolver) Comments(ctx context.Context, issueID string) ([]*Comment, error) {
	comments, err := r.CommentSvc.ListByIssue(parseID(issueID))
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	result := make([]*Comment, len(comments))
	for i, c := range comments {
		result[i] = commentFromModel(&c)
	}
	return result, nil
}

func (r *queryResolver) Timeline(ctx context.Context, issueID string) ([]*TimelineEvent, error) {
	events, err := r.IssueSvc.ListTimeline(parseID(issueID))
	if err != nil {
		return nil, fmt.Errorf("list timeline: %w", err)
	}
	result := make([]*TimelineEvent, len(events))
	for i, e := range events {
		result[i] = timelineFromModel(&e)
	}
	return result, nil
}

func (r *queryResolver) ValidTransitions(ctx context.Context, state IssueState) ([]IssueState, error) {
	states, err := r.WorkflowSvc.ValidTransitions(models.IssueState(state))
	if err != nil {
		return nil, fmt.Errorf("valid transitions: %w", err)
	}
	result := make([]IssueState, len(states))
	for i, s := range states {
		result[i] = IssueState(s)
	}
	return result, nil
}

// ---- Helpers ----

func uintPtr(v uint) *uint {
	return &v
}
