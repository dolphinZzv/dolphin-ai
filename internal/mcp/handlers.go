package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"

	"chick/internal/models"
	"chick/internal/notifications"
	"chick/internal/service"
)

type Handlers struct {
	projectSvc  *service.ProjectService
	agentSvc    *service.AgentService
	issueSvc    *service.IssueService
	commentSvc  *service.CommentService
	workflowSvc *service.WorkflowService
	feedbackSvc *service.FeedbackService
	notifSvc    *notifications.Service
}

func NewHandlers(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	workflowSvc *service.WorkflowService,
	feedbackSvc *service.FeedbackService,
	notifSvc *notifications.Service,
) *Handlers {
	return &Handlers{
		projectSvc:  projectSvc,
		agentSvc:    agentSvc,
		issueSvc:    issueSvc,
		commentSvc:  commentSvc,
		workflowSvc: workflowSvc,
		feedbackSvc: feedbackSvc,
		notifSvc:    notifSvc,
	}
}

// resolveProject resolves a project ID from an explicit projectId or the agent's single membership.
func (h *Handlers) resolveProject(projectIDStr string, agentID uint) (uint, error) {
	if projectIDStr != "" {
		pid, err := strconv.ParseUint(projectIDStr, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid projectId: %s", projectIDStr)
		}
		return uint(pid), nil
	}
	if agentID == 0 {
		return 0, fmt.Errorf("cannot determine project: not authenticated")
	}
	projects, err := h.projectSvc.ListByAgent(agentID)
	if err != nil {
		return 0, fmt.Errorf("cannot determine project: %w", err)
	}
	if len(projects) == 0 {
		return 0, fmt.Errorf("agent is not a member of any project")
	}
	if len(projects) > 1 {
		return 0, fmt.Errorf("agent is member of multiple projects, specify projectId")
	}
	return projects[0].ID, nil
}

// RegisterAll registers all MCP tools with the registry
func (h *Handlers) RegisterAll(registry *ToolRegistry) {

	registry.Register(&ToolDefinition{
		Name:        "get_agent_info",
		Description: "Get agent details by ID or external ID",
		InputSchema: ObjectSchema(map[string]interface{}{
			"agentId":    StringParam("Agent numeric ID"),
			"externalId": StringParam("Agent external ID"),
		}, nil),
		Handler: h.handleGetAgentInfo,
	})

	registry.Register(&ToolDefinition{
		Name:        "create_issue",
		Description: "Create a new issue",
		InputSchema: ObjectSchema(map[string]interface{}{
			"title":       StringRequiredParam("Issue title"),
			"description": StringParam("Issue description in Markdown"),
			"priority":    StringParam("Priority: critical / high / medium / low"),
			"assigneeIds": ArrayParam("Agent IDs to assign", "string"),
			"milestoneId": StringParam("Milestone ID to associate"),
			"projectId":   StringParam("Project ID (required if member of multiple projects)"),
		}, []string{"title"}),
		Handler: h.handleCreateIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "add_comment",
		Description: "Add a comment to an issue",
		InputSchema: ObjectSchema(map[string]interface{}{
			"issueId":  StringRequiredParam("Issue ID"),
			"body":     StringRequiredParam("Comment body (Markdown)"),
		}, []string{"issueId", "body"}),
		Handler: h.handleAddComment,
	})

	registry.Register(&ToolDefinition{
		Name:        "assign_issue",
		Description: "Assign an agent to an issue",
		InputSchema: ObjectSchema(map[string]interface{}{
			"issueId": StringRequiredParam("Issue ID"),
			"agentId": StringRequiredParam("Agent ID to assign"),
		}, []string{"issueId", "agentId"}),
		Handler: h.handleAssignIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "transition_issue",
		Description: "Transition an issue to a new state",
		InputSchema: ObjectSchema(map[string]interface{}{
			"issueId": StringRequiredParam("Issue ID"),
			"toState": StringRequiredParam("Target state: open / in_progress / blocked / review / closed_completed / closed_not_planned"),
		}, []string{"issueId", "toState"}),
		Handler: h.handleTransitionIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "search_issues",
		Description: "Search issues with filters",
		InputSchema: ObjectSchema(map[string]interface{}{
			"state":      StringParam("Filter by state"),
			"search":     StringParam("Full text search"),
			"assigneeId": StringParam("Filter by assignee agent ID"),
			"limit":      NumberParam("Max results (default 20)"),
			"offset":     NumberParam("Offset for pagination"),
			"projectId":  StringParam("Project ID (filter by project)"),
		}, nil),
		Handler: h.handleSearchIssues,
	})

	registry.Register(&ToolDefinition{
		Name:        "list_agents",
		Description: "List registered agents",
		InputSchema: ObjectSchema(map[string]interface{}{
			"kind":      StringParam("Filter by kind: ai / human / hybrid"),
			"status":    StringParam("Filter by status: online / busy / offline / error"),
			"projectId": StringParam("Project ID (filter by project)"),
		}, nil),
		Handler: h.handleListAgents,
	})

	registry.Register(&ToolDefinition{
		Name:        "agent_heartbeat",
		Description: "Update agent heartbeat timestamp",
		InputSchema: ObjectSchema(map[string]interface{}{
		}, nil),
		Handler: h.handleHeartbeat,
	})

	registry.Register(&ToolDefinition{
		Name:        "check_notifications",
		Description: "Check notifications for the authenticated agent",
		InputSchema:  ObjectSchema(map[string]interface{}{
			"projectId": StringParam("Optional: filter notifications by project ID"),
			}, nil),
		Handler: h.handleCheckNotifications,
	})

	registry.Register(&ToolDefinition{
		Name:        "submit_feedback",
		Description: "Submit feedback for an issue, comment, agent, or assignment",
		InputSchema: ObjectSchema(map[string]interface{}{
			"targetType": StringRequiredParam("Target type: issue / comment / agent / assignment"),
			"targetId":   StringRequiredParam("Target ID"),
			"rating":     StringRequiredParam("Rating 1-5"),
			"body":       StringParam("Feedback body text"),
		}, []string{"targetType", "targetId", "rating"}),
		Handler: h.handleSubmitFeedback,
	})

	registry.Register(&ToolDefinition{
		Name:        "list_feedback",
		Description: "List feedback for a target",
		InputSchema: ObjectSchema(map[string]interface{}{
			"targetType": StringRequiredParam("Target type: issue / comment / agent / assignment"),
			"targetId":   StringRequiredParam("Target ID"),
		}, []string{"targetType", "targetId"}),
		Handler: h.handleListFeedback,
	})

}

// ─── Handler Implementations ───────────────────────────────

func (h *Handlers) handleGetAgentInfo(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		AgentID    string `json:"agentId"`
		ExternalID string `json:"externalId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	var agent *models.Agent
	var err error

	if p.AgentID != "" {
		aid, err := strconv.ParseUint(p.AgentID, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid agentId: "+p.AgentID)
		}
		agent, err = h.agentSvc.GetByID(uint(aid))
	} else if p.ExternalID != "" {
		agent, err = h.agentSvc.GetByExternalID(p.ExternalID)
	} else {
		return NewError(id, -32602, "Provide agentId or externalId")
	}

	if err != nil {
		return NewInternalError(id, err.Error())
	}
	if agent == nil {
		return NewError(id, -32602, "Agent not found")
	}

	return NewResponse(id, map[string]interface{}{
		"id":           fmt.Sprintf("%d", agent.ID),
		"number":       agent.Number,
		"name":         agent.Name,
		"kind":         string(agent.Kind),
		"status":       string(agent.Status),
		"externalId":   agent.ExternalID,
		"capabilities": agent.Capabilities,
		"deviceInfo":   agent.DeviceInfo,
		"modelInfo":    agent.ModelInfo,
		"lastIp":       agent.LastIP,
		"tokenPreview": maskToken(agent.Token),
	})
}

func (h *Handlers) handleCreateIssue(id json.RawMessage, params json.RawMessage, creatorID uint, remoteAddr string) Response {
	var p struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    string   `json:"priority"`
		AssigneeIDs []string `json:"assigneeIds"`
		MilestoneID string   `json:"milestoneId"`
		ProjectID   string   `json:"projectId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	if p.Title == "" {
		return NewError(id, -32602, "Missing required param: title")
	}
	if creatorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	projectID, err := h.resolveProject(p.ProjectID, creatorID)
	if err != nil {
		return NewError(id, -32602, err.Error())
	}
	priority := models.PriorityMedium
	if p.Priority != "" {
		switch p.Priority {
		case "critical", "high", "medium", "low":
			priority = models.Priority(p.Priority)
		default:
			return NewError(id, -32602, "Invalid priority: must be critical/high/medium/low")
		}
	}

	var assigneeIDs []uint
	for _, a := range p.AssigneeIDs {
		if aid, err := strconv.ParseUint(a, 10, 64); err == nil {
			assigneeIDs = append(assigneeIDs, uint(aid))
		}
	}


		var milestoneID *uint
		if p.MilestoneID != "" {
			if mid, err := strconv.ParseUint(p.MilestoneID, 10, 64); err == nil {
				v := uint(mid)
				milestoneID = &v
			}
		}
	issue, err := h.issueSvc.Create(projectID, creatorID, p.Title, p.Description, priority, assigneeIDs, nil, milestoneID)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":     fmt.Sprintf("%d", issue.ID),
		"number": issue.Number,
		"title":  issue.Title,
		"state":  string(issue.State),
	})
}

func (h *Handlers) handleAddComment(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		IssueID string `json:"issueId"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	issueID, err := strconv.ParseUint(p.IssueID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid issueId: "+p.IssueID)
	}

	comment, err := h.commentSvc.Create(uint(issueID), authorID, p.Body, models.CommentMarkdown, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":   fmt.Sprintf("%d", comment.ID),
		"body": comment.Body,
	})
}

func (h *Handlers) handleAssignIssue(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		IssueID string `json:"issueId"`
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	issueID, err := strconv.ParseUint(p.IssueID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid issueId: "+p.IssueID)
	}
	assignAgentID, err := strconv.ParseUint(p.AgentID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid agentId: "+p.AgentID)
	}

	_, err = h.issueSvc.AddAssignee(uint(issueID), uint(assignAgentID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleTransitionIssue(id json.RawMessage, params json.RawMessage, actorID uint, remoteAddr string) Response {
	var p struct {
		IssueID string `json:"issueId"`
		ToState string `json:"toState"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if actorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	issueID, err := strconv.ParseUint(p.IssueID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid issueId: "+p.IssueID)
	}

	issue, err := h.workflowSvc.Transition(uint(issueID), models.IssueState(p.ToState), actorID, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", issue.ID),
		"state": string(issue.State),
	})
}

func (h *Handlers) handleSearchIssues(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		State      string `json:"state"`
		Search     string `json:"search"`
		AssigneeID string `json:"assigneeId"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
		ProjectID  string `json:"projectId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	filter := models.IssueFilter{
		Search: p.Search,
		Limit:  p.Limit,
		Offset: p.Offset,
	}
	if p.Limit <= 0 {
		filter.Limit = 20
	}
	if p.State != "" {
		filter.State = []models.IssueState{models.IssueState(p.State)}
	}
	if p.ProjectID != "" {
		pid, err := strconv.ParseUint(p.ProjectID, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid projectId: "+p.ProjectID)
		}
		v := uint(pid)
		filter.ProjectID = &v
	} else if agentID > 0 {
		projects, err := h.projectSvc.ListByAgent(agentID)
		if err == nil && len(projects) == 1 {
			v := projects[0].ID
			filter.ProjectID = &v
		}
	}
	if p.AssigneeID != "" {
		aid, err := strconv.ParseUint(p.AssigneeID, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid assigneeId: "+p.AssigneeID)
		}
		filter.AssigneeIDs = []uint{uint(aid)}
	}

	issues, total, err := h.issueSvc.List(filter)
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	items := make([]map[string]interface{}, len(issues))
	for i, issue := range issues {
		items[i] = map[string]interface{}{
			"id":     fmt.Sprintf("%d", issue.ID),
			"number": issue.Number,
			"title":  issue.Title,
			"state":  string(issue.State),
		}
	}
	return NewResponse(id, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

func (h *Handlers) handleListAgents(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	filter := models.AgentFilter{}
	if p.Kind != "" {
		v := models.AgentKind(p.Kind)
		filter.Kind = &v
	}
	if p.Status != "" {
		v := models.AgentStatus(p.Status)
		filter.Status = &v
	}
	if p.ProjectID != "" {
		pid, err := strconv.ParseUint(p.ProjectID, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid projectId: "+p.ProjectID)
		}
		v := uint(pid)
		filter.ProjectID = &v
	} else if agentID > 0 {
		projects, err := h.projectSvc.ListByAgent(agentID)
		if err == nil && len(projects) == 1 {
			v := projects[0].ID
			filter.ProjectID = &v
		}
	}
	agents, err := h.agentSvc.List(filter)
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	items := make([]map[string]interface{}, len(agents))
	for i, a := range agents {
		items[i] = map[string]interface{}{
			"number": a.Number,
			"id":     fmt.Sprintf("%d", a.ID),
			"name":   a.Name,
			"kind":   string(a.Kind),
			"status": string(a.Status),
		}
	}
	return NewResponse(id, map[string]interface{}{"items": items})
}

func (h *Handlers) handleHeartbeat(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if err := h.agentSvc.Heartbeat(agentID); err != nil {
		return NewInternalError(id, err.Error())
	}
	if remoteAddr != "" {
		h.agentSvc.UpdateIP(agentID, remoteAddr)
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleCheckNotifications(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}

	if h.notifSvc == nil {
		return NewResponse(id, map[string]interface{}{"notifications": []interface{}{}})
	}

	var p struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		// params is optional, ignore unmarshal errors
	}

	var notifs []notifications.Notification
	if p.ProjectID != "" {
		pid, err := strconv.ParseUint(p.ProjectID, 10, 64)
		if err == nil && pid > 0 {
			notifs = h.notifSvc.ListByAgent(agentID, uint(pid))
		} else {
			notifs = h.notifSvc.ListByAgent(agentID)
		}
	} else {
		notifs = h.notifSvc.ListByAgent(agentID)
	}

	items := make([]map[string]interface{}, len(notifs))
	for i, n := range notifs {
		items[i] = map[string]interface{}{
			"id":        n.ID,
			"type":      string(n.Type),
			"issueId":   n.IssueID,
			"projectId": n.ProjectID,
			"message":   n.Message,
			"read":      n.Read,
			"createdAt": n.CreatedAt,
		}
	}
	return NewResponse(id, map[string]interface{}{"notifications": items})
}

func (h *Handlers) handleSubmitFeedback(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		Rating     string `json:"rating"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}

	targetID, err := strconv.ParseUint(p.TargetID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid targetId: "+p.TargetID)
	}
	rating, err := strconv.Atoi(p.Rating)
	if err != nil || rating < 1 || rating > 5 {
		return NewError(id, -32602, "Invalid rating: must be 1-5")
	}

	feedback, err := h.feedbackSvc.Create(
		models.FeedbackTargetType(p.TargetType),
		uint(targetID),
		authorID,
		rating,
		p.Body,
	)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":     feedback.ID,
		"rating": feedback.Rating,
		"body":   feedback.Body,
	})
}

func (h *Handlers) handleListFeedback(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	targetID, err := strconv.ParseUint(p.TargetID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid targetId: "+p.TargetID)
	}

	items, err := h.feedbackSvc.ListByTarget(models.FeedbackTargetType(p.TargetType), uint(targetID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	result := make([]map[string]interface{}, len(items))
	for i, f := range items {
		result[i] = map[string]interface{}{
			"id":       f.ID,
			"rating":   f.Rating,
			"body":     f.Body,
			"authorId": f.AuthorID,
		}
	}
	return NewResponse(id, map[string]interface{}{"items": result})
}

func maskToken(token string) string {
	if len(token) <= 10 {
		return token
	}
	return token[:6] + "…" + token[len(token)-4:]
}
