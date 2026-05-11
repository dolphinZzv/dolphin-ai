package graph

import (
	"strconv"

	"chick/internal/models"
)

func parseID(s string) uint {
	id, _ := strconv.ParseUint(s, 10, 64)
	return uint(id)
}

func formatID(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}

func agentFromModel(a *models.Agent) *Agent {
	if a == nil {
		return nil
	}
	agent := &Agent{
		ID:           formatID(a.ID),
		Name:         a.Name,
		Kind:         AgentKind(a.Kind),
		Status:       AgentStatus(a.Status),
		ExternalID:   a.ExternalID,
		Capabilities: []string(a.Capabilities),
		LastSeenAt:   a.LastSeenAt,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
	return agent
}

func projectFromModel(p *models.Project) *Project {
	if p == nil {
		return nil
	}
	proj := &Project{
		ID:          formatID(p.ID),
		Name:        p.Name,
		Description: strPtr(p.Description),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	if len(p.Members) > 0 {
		members := make([]*ProjectMember, len(p.Members))
		for i, m := range p.Members {
			members[i] = projectMemberFromModel(&m)
		}
		proj.Members = members
	}
	return proj
}

func projectMemberFromModel(m *models.ProjectMember) *ProjectMember {
	return &ProjectMember{
		ID:        formatID(m.ID),
		ProjectID: formatID(m.ProjectID),
		AgentID:   formatID(m.AgentID),
		Role:      ProjectRole(m.Role),
	}
}

func issueFromModel(i *models.Issue) *Issue {
	if i == nil {
		return nil
	}
	issue := &Issue{
		ID:          formatID(i.ID),
		Number:      int32(i.Number),
		ProjectID:   formatID(i.ProjectID),
		Title:       i.Title,
		Description: strPtr(i.Description),
		State:       IssueState(i.State),
		Priority:    Priority(i.Priority),
		CreatorID:   formatID(i.CreatorID),
		DueDate:     i.DueDate,
		ClosedAt:    i.ClosedAt,
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
		Creator:     agentFromModel(&i.Creator),
	}
	if i.ParentID != nil {
		pid := formatID(*i.ParentID)
		issue.ParentID = &pid
	}
	if len(i.Assignees) > 0 {
		assignees := make([]*IssueAssignee, len(i.Assignees))
		for j, a := range i.Assignees {
			assignees[j] = issueAssigneeFromModel(&a)
		}
		issue.Assignees = assignees
	}
	if len(i.Labels) > 0 {
		labels := make([]*Label, len(i.Labels))
		for j, l := range i.Labels {
			labels[j] = labelFromModel(&l)
		}
		issue.Labels = labels
	}
	return issue
}

func issueAssigneeFromModel(ia *models.IssueAssignee) *IssueAssignee {
	return &IssueAssignee{
		ID:      formatID(ia.ID),
		IssueID: formatID(ia.IssueID),
		AgentID: formatID(ia.AgentID),
		State:   AssigneeState(ia.State),
		Agent:   agentFromModel(&ia.Agent),
	}
}

func labelFromModel(l *models.Label) *Label {
	return &Label{
		ID:        formatID(l.ID),
		Name:      l.Name,
		Color:     strPtr(l.Color),
		ProjectID: formatID(l.ProjectID),
	}
}

func commentFromModel(c *models.Comment) *Comment {
	if c == nil {
		return nil
	}
	comment := &Comment{
		ID:          formatID(c.ID),
		IssueID:     formatID(c.IssueID),
		AuthorID:    formatID(c.AuthorID),
		Body:        c.Body,
		ContentType: CommentContentType(c.ContentType),
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
		Author:      agentFromModel(&c.Author),
	}
	if c.ParentID != nil {
		pid := formatID(*c.ParentID)
		comment.ParentID = &pid
	}
	return comment
}

func timelineFromModel(t *models.TimelineEvent) *TimelineEvent {
	return &TimelineEvent{
		ID:        formatID(t.ID),
		IssueID:   formatID(t.IssueID),
		ActorID:   formatID(t.ActorID),
		EventType: string(t.EventType),
		CreatedAt: t.CreatedAt,
		Actor:     agentFromModel(&t.Actor),
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
