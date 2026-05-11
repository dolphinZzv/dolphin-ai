package graph

import (
	"context"
	"fmt"
	"time"

	"chick/internal/events"
	"chick/internal/models"
)

// mutationResolver implements MutationResolver.
type mutationResolver struct{ *Resolver }

// queryResolver implements QueryResolver.
type queryResolver struct{ *Resolver }

// subscriptionResolver implements SubscriptionResolver.
type subscriptionResolver struct{ *Resolver }

func (r *Resolver) Mutation() MutationResolver       { return &mutationResolver{r} }
func (r *Resolver) Query() QueryResolver             { return &queryResolver{r} }
func (r *Resolver) Subscription() SubscriptionResolver { return &subscriptionResolver{r} }

// ─── Agent Mutations ─────────────────────────────────────────────

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

func (r *mutationResolver) UpdateAgentStatus(ctx context.Context, id string, status AgentStatus) (*Agent, error) {
	if err := r.AgentSvc.UpdateStatus(parseID(id), models.AgentStatus(status)); err != nil {
		return nil, fmt.Errorf("update agent status: %w", err)
	}
	a, err := r.AgentSvc.GetByID(parseID(id))
	if err != nil {
		return nil, err
	}
	return agentFromModel(a), nil
}

// ─── Project Mutations ───────────────────────────────────────────

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

func (r *mutationResolver) UpdateProject(ctx context.Context, id string, name *string, description *string) (*Project, error) {
	n := ""
	if name != nil {
		n = *name
	}
	d := ""
	if description != nil {
		d = *description
	}
	p, err := r.ProjectSvc.Update(parseID(id), n, d)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return projectFromModel(p), nil
}

func (r *mutationResolver) DeleteProject(ctx context.Context, id string) (bool, error) {
	if err := r.ProjectSvc.Delete(parseID(id)); err != nil {
		return false, fmt.Errorf("delete project: %w", err)
	}
	return true, nil
}

func (r *mutationResolver) AddProjectMember(ctx context.Context, projectID string, agentID string, role ProjectRole) (*ProjectMember, error) {
	m, err := r.ProjectSvc.AddMember(parseID(projectID), parseID(agentID), models.ProjectRole(role))
	if err != nil {
		return nil, fmt.Errorf("add project member: %w", err)
	}
	return projectMemberFromModel(m), nil
}

func (r *mutationResolver) UpdateProjectMember(ctx context.Context, projectID string, agentID string, role ProjectRole) (*ProjectMember, error) {
	m, err := r.ProjectSvc.UpdateMember(parseID(projectID), parseID(agentID), models.ProjectRole(role))
	if err != nil {
		return nil, fmt.Errorf("update project member: %w", err)
	}
	return projectMemberFromModel(m), nil
}

func (r *mutationResolver) RemoveProjectMember(ctx context.Context, projectID string, agentID string) (bool, error) {
	if err := r.ProjectSvc.RemoveMember(parseID(projectID), parseID(agentID)); err != nil {
		return false, fmt.Errorf("remove project member: %w", err)
	}
	return true, nil
}

// ─── Issue Mutations ─────────────────────────────────────────────

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

func (r *mutationResolver) UpdateIssue(ctx context.Context, id string, title *string, description *string, priority *Priority, dueDate *time.Time) (*Issue, error) {
	t := ""
	if title != nil {
		t = *title
	}
	d := ""
	if description != nil {
		d = *description
	}
	p := models.Priority("")
	if priority != nil {
		p = models.Priority(*priority)
	}
	var nt *models.UnixNullTime
	if dueDate != nil {
		nt = &models.UnixNullTime{Time: *dueDate, Valid: true}
	}
	issue, err := r.IssueSvc.Update(parseID(id), t, d, p, nt)
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}
	return issueFromModel(issue), nil
}

func (r *mutationResolver) DeleteIssue(ctx context.Context, id string) (bool, error) {
	if err := r.IssueSvc.Delete(parseID(id)); err != nil {
		return false, fmt.Errorf("delete issue: %w", err)
	}
	return true, nil
}

func (r *mutationResolver) TransitionIssue(ctx context.Context, id string, newState IssueState, actorID string) (*Issue, error) {
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

func (r *mutationResolver) UpdateAssigneeState(ctx context.Context, issueID string, agentID string, state AssigneeState) (*IssueAssignee, error) {
	ia, err := r.IssueSvc.UpdateAssigneeState(parseID(issueID), parseID(agentID), models.AssigneeState(state))
	if err != nil {
		return nil, fmt.Errorf("update assignee state: %w", err)
	}
	return issueAssigneeFromModel(ia), nil
}

// ─── Comment Mutations ───────────────────────────────────────────

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

func (r *mutationResolver) UpdateComment(ctx context.Context, id string, body string) (*Comment, error) {
	c, err := r.CommentSvc.Update(parseID(id), body)
	if err != nil {
		return nil, fmt.Errorf("update comment: %w", err)
	}
	return commentFromModel(c), nil
}

func (r *mutationResolver) DeleteComment(ctx context.Context, id string) (bool, error) {
	if err := r.CommentSvc.Delete(parseID(id)); err != nil {
		return false, fmt.Errorf("delete comment: %w", err)
	}
	return true, nil
}

// ─── Label Mutations ─────────────────────────────────────────────

func (r *mutationResolver) AddLabels(ctx context.Context, issueID string, labelIDs []string) (*Issue, error) {
	for _, lid := range labelIDs {
		if err := r.IssueSvc.AddLabel(parseID(issueID), parseID(lid)); err != nil {
			return nil, fmt.Errorf("add label: %w", err)
		}
	}
	issue, err := r.IssueSvc.GetByID(parseID(issueID))
	if err != nil {
		return nil, err
	}
	return issueFromModel(issue), nil
}

func (r *mutationResolver) RemoveLabels(ctx context.Context, issueID string, labelIDs []string) (*Issue, error) {
	for _, lid := range labelIDs {
		if err := r.IssueSvc.RemoveLabel(parseID(issueID), parseID(lid)); err != nil {
			return nil, fmt.Errorf("remove label: %w", err)
		}
	}
	issue, err := r.IssueSvc.GetByID(parseID(issueID))
	if err != nil {
		return nil, err
	}
	return issueFromModel(issue), nil
}

func (r *mutationResolver) CreateLabel(ctx context.Context, projectID string, name string, color *string, capability *string, group *string) (*Label, error) {
	c := ""
	if color != nil {
		c = *color
	}
	cap := ""
	if capability != nil {
		cap = *capability
	}
	g := ""
	if group != nil {
		g = *group
	}
	l, err := r.ProjectSvc.CreateLabel(parseID(projectID), name, c, cap, g)
	if err != nil {
		return nil, fmt.Errorf("create label: %w", err)
	}
	return labelFromModel(l), nil
}

func (r *mutationResolver) DeleteLabel(ctx context.Context, id string) (bool, error) {
	if err := r.ProjectSvc.DeleteLabel(parseID(id)); err != nil {
		return false, fmt.Errorf("delete label: %w", err)
	}
	return true, nil
}

// ─── Milestone Mutations ─────────────────────────────────────────

func (r *mutationResolver) CreateMilestone(ctx context.Context, projectID string, title string, description *string, dueDate *time.Time) (*Milestone, error) {
	d := ""
	if description != nil {
		d = *description
	}
	var nt *models.UnixNullTime
	if dueDate != nil {
		nt = &models.UnixNullTime{Time: *dueDate, Valid: true}
	}
	m, err := r.ProjectSvc.CreateMilestone(parseID(projectID), title, d, nt)
	if err != nil {
		return nil, fmt.Errorf("create milestone: %w", err)
	}
	return milestoneFromModel(m), nil
}

func (r *mutationResolver) DeleteMilestone(ctx context.Context, id string) (bool, error) {
	if err := r.ProjectSvc.DeleteMilestone(parseID(id)); err != nil {
		return false, fmt.Errorf("delete milestone: %w", err)
	}
	return true, nil
}

// ─── Feedback Mutations ──────────────────────────────────────────

func (r *mutationResolver) CreateFeedback(ctx context.Context, targetType FeedbackTargetType, targetID string, authorID string, rating FeedbackRating, body *string) (*Feedback, error) {
	b := ""
	if body != nil {
		b = *body
	}
	f, err := r.FeedbackSvc.Create(models.FeedbackTargetType(targetType), parseID(targetID), parseID(authorID), feedbackRatingToInt(rating), b)
	if err != nil {
		return nil, fmt.Errorf("create feedback: %w", err)
	}
	return feedbackFromModel(f), nil
}

// ─── Queries ─────────────────────────────────────────────────────

func (r *queryResolver) Agents(ctx context.Context, kind *AgentKind, status *AgentStatus, capabilities []string, projectID *string) ([]*Agent, error) {
	filter := models.AgentFilter{}
	if kind != nil {
		k := models.AgentKind(*kind)
		filter.Kind = &k
	}
	if status != nil {
		s := models.AgentStatus(*status)
		filter.Status = &s
	}
	if projectID != nil {
		pid := parseID(*projectID)
		filter.ProjectID = &pid
	}
	if len(capabilities) > 0 {
		caps := make([]models.CapabilityType, len(capabilities))
		for i, c := range capabilities {
			caps[i] = models.CapabilityType(c)
		}
		filter.Capabilities = caps
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

func (r *queryResolver) Agent(ctx context.Context, id string) (*Agent, error) {
	a, err := r.AgentSvc.GetByID(parseID(id))
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	return agentFromModel(a), nil
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

func (r *queryResolver) Labels(ctx context.Context, projectID string, group *string) ([]*Label, error) {
	g := ""
	if group != nil {
		g = *group
	}
	labels, err := r.ProjectSvc.ListLabels(parseID(projectID), g)
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	result := make([]*Label, len(labels))
	for i, l := range labels {
		result[i] = labelFromModel(&l)
	}
	return result, nil
}

func (r *queryResolver) Milestones(ctx context.Context, projectID string, state *MilestoneState) ([]*Milestone, error) {
	s := models.MilestoneState("")
	if state != nil {
		s = models.MilestoneState(*state)
	}
	milestones, err := r.ProjectSvc.ListMilestones(parseID(projectID), s)
	if err != nil {
		return nil, fmt.Errorf("list milestones: %w", err)
	}
	result := make([]*Milestone, len(milestones))
	for i, m := range milestones {
		result[i] = milestoneFromModel(&m)
	}
	return result, nil
}

func (r *queryResolver) Skills(ctx context.Context, projectID string) ([]*Skill, error) {
	// Skills not yet implemented via service, return empty
	return []*Skill{}, nil
}

func (r *queryResolver) Issue(ctx context.Context, id string) (*Issue, error) {
	issue, err := r.IssueSvc.GetByID(parseID(id))
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	return issueFromModel(issue), nil
}

func (r *queryResolver) Issues(ctx context.Context, projectID string, state *IssueState, priority *Priority, assigneeID *string, limit *int32, offset *int32) (*IssueConnection, error) {
	filter := models.IssueFilter{ProjectID: uintPtr(parseID(projectID))}
	if state != nil {
		filter.State = []models.IssueState{models.IssueState(*state)}
	}
	if priority != nil {
		p := models.Priority(*priority)
		filter.Priority = &p
	}
	if assigneeID != nil {
		filter.AssigneeIDs = []uint{parseID(*assigneeID)}
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
	for i, iss := range issues {
		result[i] = issueFromModel(&iss)
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

func (r *queryResolver) Feedback(ctx context.Context, targetType FeedbackTargetType, targetID string) ([]*Feedback, error) {
	list, err := r.FeedbackSvc.ListByTarget(models.FeedbackTargetType(targetType), parseID(targetID))
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	result := make([]*Feedback, len(list))
	for i, f := range list {
		result[i] = feedbackFromModel(&f)
	}
	return result, nil
}

// ─── Subscriptions ───────────────────────────────────────────────

func (r *subscriptionResolver) IssueUpdated(ctx context.Context, issueID string) (<-chan *Issue, error) {
	ch := make(chan *Issue, 1)
	id := parseID(issueID)

	// Publish initial state
	issue, err := r.IssueSvc.GetByID(id)
	if err == nil {
		ch <- issueFromModel(issue)
	}

	if r.EventBus != nil {
		r.EventBus.Subscribe(events.EventIssueStateChanged, func(evt events.Event) {
			payload, ok := evt.Payload.(map[string]interface{})
			if !ok {
				return
			}
			eid, _ := payload["issueID"].(uint)
			if eid == id {
				issue, err := r.IssueSvc.GetByID(id)
				if err == nil {
					select {
					case ch <- issueFromModel(issue):
					default:
					}
				}
			}
		})
	}

	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch, nil
}

func (r *subscriptionResolver) AgentNotifications(ctx context.Context, agentID string) (<-chan *NotificationEvent, error) {
	ch := make(chan *NotificationEvent, 5)
	id := parseID(agentID)

	if r.EventBus != nil {
		r.EventBus.Subscribe(events.EventIssueAssigneeChanged, func(evt events.Event) {
			payload, ok := evt.Payload.(map[string]interface{})
			if !ok {
				return
			}
			aid, _ := payload["agentID"].(uint)
			if aid == id {
				msg := "You have been assigned to an issue"
				action, _ := payload["action"].(string)
				if action == "removed" {
					msg = "You have been unassigned from an issue"
				}
				select {
				case ch <- &NotificationEvent{
					ID:               fmt.Sprintf("notif-%d", time.Now().UnixNano()),
					NotificationType: "assignee_changed",
					Message:          msg,
					Read:             false,
					CreatedAt:        time.Now(),
				}:
				default:
				}
			}
		})

		r.EventBus.Subscribe(events.EventCommentAdded, func(evt events.Event) {
			payload, ok := evt.Payload.(map[string]interface{})
			if !ok {
				return
			}
			_ = payload["commentID"]
			select {
			case ch <- &NotificationEvent{
				ID:               fmt.Sprintf("notif-%d", time.Now().UnixNano()),
				NotificationType: "comment_added",
				Message:          "New comment on an issue",
				Read:             false,
				CreatedAt:        time.Now(),
			}:
			default:
			}
		})
	}

	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch, nil
}

func (r *subscriptionResolver) AgentStatusChanged(ctx context.Context) (<-chan *AgentStatusEvent, error) {
	ch := make(chan *AgentStatusEvent, 5)

	if r.EventBus != nil {
		r.EventBus.Subscribe(events.EventAgentStatusChanged, func(evt events.Event) {
			payload, ok := evt.Payload.(map[string]interface{})
			if !ok {
				return
			}
			aid, _ := payload["agentID"].(uint)
			status, _ := payload["status"].(string)
			select {
			case ch <- &AgentStatusEvent{
				AgentID:   formatID(aid),
				Status:    AgentStatus(models.AgentStatus(status)),
				Timestamp: time.Now(),
			}:
			default:
			}
		})
	}

	go func() {
		<-ctx.Done()
		close(ch)
	}()

	return ch, nil
}
