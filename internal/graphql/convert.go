package graph

import (
	"crypto/rand"
	"encoding/hex"
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
		Number:       int32(a.Number),
		Name:         a.Name,
		Kind:         AgentKind(a.Kind),
		Status:       AgentStatus(a.Status),
		Disabled:     a.Disabled,
		ExternalID:   a.ExternalID,
		Capabilities: []string(a.Capabilities),
		AllowedCIDRs: []string(a.AllowedCIDRs),
		LastSeenAt:   a.LastSeenAt,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
	if a.SystemPrompt != "" {
		agent.SystemPrompt = &a.SystemPrompt
	}
	if a.Metadata != nil {
		agent.Metadata = map[string]any(a.Metadata)
	}
	if a.DeviceInfo != "" {
		agent.DeviceInfo = &a.DeviceInfo
	}
	if a.ModelInfo != "" {
		agent.ModelInfo = &a.ModelInfo
	}
	if a.LastIP != "" {
		agent.LastIP = &a.LastIP
	}
	if a.Token != "" {
		agent.TokenPreview = strPtr(maskToken(a.Token))
	}
	return agent
}

func projectFromModel(p *models.Project) *Project {
	if p == nil {
		return nil
	}
	proj := &Project{
		ID:                          formatID(p.ID),
		Name:                        p.Name,
		Description:                 strPtr(p.Description),
		AllowCreatorTransition:      p.AllowCreatorTransition,
		RequireCreatorCloseApproval: p.RequireCreatorCloseApproval,
		CreatedAt:                   p.CreatedAt,
		UpdatedAt:                   p.UpdatedAt,
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
	pm := &ProjectMember{
		ID:        formatID(m.ID),
		ProjectID: formatID(m.ProjectID),
		AgentID:   formatID(m.AgentID),
		Role:      ProjectRole(m.Role),
	}
	if m.Agent.ID != 0 {
		pm.Agent = agentFromModel(&m.Agent)
	}
	return pm
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
		Environment: i.Environment,
		Branch:      i.Branch,
		Link:        i.Link,
		ClosedAt:    i.ClosedAt,
		StartedAt:   i.StartedAt,
		CompletedAt: i.CompletedAt,
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
	if len(i.Children) > 0 {
		children := make([]*Issue, len(i.Children))
		for j, c := range i.Children {
			children[j] = issueFromModel(&c)
		}
		issue.Children = children
	}
	if i.Milestone != nil {
		issue.Milestone = milestoneFromModel(i.Milestone)
	}
	if i.StructuredOutput != nil {
		issue.StructuredOutput = map[string]any(i.StructuredOutput)
	}
	if i.Difficulty != nil {
		d := int32(*i.Difficulty)
		issue.Difficulty = &d
	}
	return issue
}

func issueAssigneeFromModel(ia *models.IssueAssignee) *IssueAssignee {
	return &IssueAssignee{
		ID:         formatID(ia.ID),
		IssueID:    formatID(ia.IssueID),
		AgentID:    formatID(ia.AgentID),
		State:      AssigneeState(ia.State),
		AssignedAt: ia.AssignedAt,
		Agent:      agentFromModel(&ia.Agent),
	}
}

func labelFromModel(l *models.Label) *Label {
	if l == nil {
		return nil
	}
	label := &Label{
		ID:          formatID(l.ID),
		Name:        l.Name,
		Description: strPtr(l.Description),
		ProjectID:   formatID(l.ProjectID),
	}
	if l.Color != "" {
		label.Color = &l.Color
	}
	if l.Capability != "" {
		c := string(l.Capability)
		label.Capability = &c
	}
	if l.Group != "" {
		label.Group = &l.Group
	}
	return label
}

func milestoneFromModel(m *models.Milestone) *Milestone {
	if m == nil {
		return nil
	}
	return &Milestone{
		ID:          formatID(m.ID),
		ProjectID:   formatID(m.ProjectID),
		Title:       m.Title,
		Description: strPtr(m.Description),
		State:       MilestoneState(m.State),
		DueDate:     m.DueDate,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func commentFromModel(c *models.Comment) *Comment {
	if c == nil {
		return nil
	}
	comment := &Comment{
		ID:          formatID(c.ID),
		AuthorID:    formatID(c.AuthorID),
		Body:        c.Body,
		ContentType: CommentContentType(c.ContentType),
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
		Author:      agentFromModel(&c.Author),
	}
	if c.IssueID != nil {
		id := formatID(*c.IssueID)
		comment.IssueID = &id
	}
	if c.ProposalID != nil {
		pid := formatID(*c.ProposalID)
		comment.ProposalID = &pid
	}
	if c.TaskID != nil {
		tid := formatID(*c.TaskID)
		comment.TaskID = &tid
	}
	if c.ParentID != nil {
		pid := formatID(*c.ParentID)
		comment.ParentID = &pid
	}
	if c.Parent != nil {
		comment.Parent = commentFromModel(c.Parent)
	}
	if len(c.Replies) > 0 {
		replies := make([]*Comment, len(c.Replies))
		for i, r := range c.Replies {
			replies[i] = commentFromModel(&r)
		}
		comment.Replies = replies
	}
	return comment
}

func timelineFromModel(t *models.TimelineEvent) *TimelineEvent {
	te := &TimelineEvent{
		ID:        formatID(t.ID),
		ActorID:   formatID(t.ActorID),
		EventType: string(t.EventType),
		CreatedAt: t.CreatedAt,
		Actor:     agentFromModel(&t.Actor),
	}
	if t.IssueID != nil {
		id := formatID(*t.IssueID)
		te.IssueID = &id
	}
	if t.ProposalID != nil {
		pid := formatID(*t.ProposalID)
		te.ProposalID = &pid
	}
	if t.TaskID != nil {
		tid := formatID(*t.TaskID)
		te.TaskID = &tid
	}
	if t.Payload != nil {
		te.Payload = map[string]any(t.Payload)
	}
	return te
}

func proposalFromModel(p *models.Proposal) *Proposal {
	if p == nil {
		return nil
	}
	proposal := &Proposal{
		ID:          formatID(p.ID),
		Number:      int32(p.Number),
		ProjectID:   formatID(p.ProjectID),
		Title:       p.Title,
		Description: strPtr(p.Description),
		State:       ProposalState(p.State),
		Priority:    Priority(p.Priority),
		AuthorID:    formatID(p.AuthorID),
		SubmittedAt: p.SubmittedAt,
		ApprovedAt:  p.ApprovedAt,
		StartedAt:   p.StartedAt,
		CompletedAt: p.CompletedAt,
		CancelledAt: p.CancelledAt,
		ReviewedAt:  p.ReviewedAt,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
		Author:      agentFromModel(&p.Author),
	}
	if p.ReviewerID != nil {
		rid := formatID(*p.ReviewerID)
		proposal.ReviewerID = &rid
	}
	if p.ReviewNote != nil {
		proposal.ReviewNote = p.ReviewNote
	}
	if p.Reviewer != nil {
		proposal.Reviewer = agentFromModel(p.Reviewer)
	}
	if len(p.Labels) > 0 {
		labels := make([]*Label, len(p.Labels))
		for i, l := range p.Labels {
			labels[i] = labelFromModel(&l)
		}
		proposal.Labels = labels
	}
	if len(p.Tasks) > 0 {
		tasks := make([]*Task, len(p.Tasks))
		for i, t := range p.Tasks {
			tasks[i] = taskFromModel(&t)
		}
		proposal.Tasks = tasks
	}
	return proposal
}

func taskFromModel(t *models.Task) *Task {
	if t == nil {
		return nil
	}
	task := &Task{
		ID:          formatID(t.ID),
		Number:      int32(t.Number),
		ProposalID:  formatID(t.ProposalID),
		Title:       t.Title,
		Description: strPtr(t.Description),
		State:       TaskState(t.State),
		Priority:    Priority(t.Priority),
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		Assignee:    agentFromModel(t.Assignee),
	}
	if t.AssigneeID != nil {
		aid := formatID(*t.AssigneeID)
		task.AssigneeID = &aid
	}
	if t.Proposal.ID != 0 {
		task.Proposal = proposalFromModel(&t.Proposal)
	}
	if len(t.Issues) > 0 {
		issues := make([]*Issue, len(t.Issues))
		for i, iss := range t.Issues {
			issues[i] = issueFromModel(&iss)
		}
		task.Issues = issues
	}
	return task
}

func feedbackFromModel(f *models.Feedback) *Feedback {
	if f == nil {
		return nil
	}
	fb := &Feedback{
		ID:         formatID(f.ID),
		TargetType: FeedbackTargetType(f.TargetType),
		TargetID:   formatID(f.TargetID),
		AuthorID:   formatID(f.AuthorID),
		Rating:     feedbackRatingFromInt(f.Rating),
		CreatedAt:  f.CreatedAt,
		Author:     agentFromModel(&f.Author),
	}
	if f.Body != "" {
		fb.Body = &f.Body
	}
	return fb
}

func feedbackRatingFromInt(rating int) FeedbackRating {
	switch {
	case rating <= 1:
		return FeedbackRatingOne
	case rating == 2:
		return FeedbackRatingTwo
	case rating == 3:
		return FeedbackRatingThree
	case rating == 4:
		return FeedbackRatingFour
	default:
		return FeedbackRatingFive
	}
}

func feedbackRatingToInt(rating FeedbackRating) int {
	switch rating {
	case FeedbackRatingOne:
		return 1
	case FeedbackRatingTwo:
		return 2
	case FeedbackRatingThree:
		return 3
	case FeedbackRatingFour:
		return 4
	case FeedbackRatingFive:
		return 5
	default:
		return 3
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func uintPtr(v uint) *uint {
	return &v
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func maskToken(token string) string {
	if len(token) <= 10 {
		return token
	}
	return token[:6] + "…" + token[len(token)-4:]
}
