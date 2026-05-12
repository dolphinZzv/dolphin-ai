package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"

	"chick/internal/auth"
	"chick/internal/models"
	"chick/internal/notifications"
	"chick/internal/service"
)

type Handlers struct {
	projectSvc   *service.ProjectService
	agentSvc     *service.AgentService
	issueSvc     *service.IssueService
	commentSvc   *service.CommentService
	workflowSvc  *service.WorkflowService
	feedbackSvc  *service.FeedbackService
	skillSvc     *service.SkillService
	notifSvc     *notifications.Service
	auth         *auth.Authenticator
}

func NewHandlers(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	workflowSvc *service.WorkflowService,
	feedbackSvc *service.FeedbackService,
	skillSvc *service.SkillService,
	notifSvc *notifications.Service,
	auth *auth.Authenticator,
) *Handlers {
	return &Handlers{
		projectSvc:   projectSvc,
		agentSvc:     agentSvc,
		issueSvc:     issueSvc,
		commentSvc:   commentSvc,
		workflowSvc:  workflowSvc,
		feedbackSvc:  feedbackSvc,
		skillSvc:     skillSvc,
		notifSvc:     notifSvc,
		auth:         auth,
	}
}

// RegisterAll registers all MCP tools with the registry
func (h *Handlers) RegisterAll(registry *ToolRegistry) {
	registry.Register(&ToolDefinition{
		Name:        "create_project",
		Description: "Create a new project",
		InputSchema: ObjectSchema(map[string]interface{}{
			"name":        StringRequiredParam("Project name"),
			"description": StringParam("Project description"),
		}, []string{"name"}),
		Handler: h.handleCreateProject,
	})

	registry.Register(&ToolDefinition{
		Name:        "register_agent",
		Description: "Register a new agent (AI or Human)",
		InputSchema: ObjectSchema(map[string]interface{}{
			"name":         StringRequiredParam("Agent name"),
			"kind":         StringRequiredParam("Agent kind: ai / human / hybrid"),
			"externalId":   StringRequiredParam("Unique external identifier"),
			"secret":       StringRequiredParam("Password or API secret"),
			"capabilities": ArrayParam("List of capabilities", "string"),
			"bootstrapToken": StringParam("Bootstrap token for first AI agent registration"),
		}, []string{"name", "kind", "externalId", "secret"}),
		Handler: h.handleRegisterAgent,
	})

	registry.Register(&ToolDefinition{
		Name:        "login_agent",
		Description: "Login and get a JWT token",
		InputSchema: ObjectSchema(map[string]interface{}{
			"externalId": StringRequiredParam("Agent external ID"),
			"secret":     StringRequiredParam("Agent secret"),
		}, []string{"externalId", "secret"}),
		Handler: h.handleLoginAgent,
	})

	registry.Register(&ToolDefinition{
		Name:        "create_issue",
		Description: "Create a new issue in a project",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId":   StringRequiredParam("Project ID"),
			"title":       StringRequiredParam("Issue title"),
			"description": StringParam("Issue description in Markdown"),
			"creatorId":   StringRequiredParam("Creator agent ID"),
			"priority":    StringParam("Priority: critical / high / medium / low"),
			"assigneeIds": ArrayParam("Agent IDs to assign", "string"),
		}, []string{"projectId", "title", "creatorId"}),
		Handler: h.handleCreateIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "add_comment",
		Description: "Add a comment to an issue",
		InputSchema: ObjectSchema(map[string]interface{}{
			"issueId": StringRequiredParam("Issue ID"),
			"authorId": StringRequiredParam("Author agent ID"),
			"body":    StringRequiredParam("Comment body (Markdown)"),
		}, []string{"issueId", "authorId", "body"}),
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
			"actorId": StringRequiredParam("Actor agent ID"),
		}, []string{"issueId", "toState", "actorId"}),
		Handler: h.handleTransitionIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "search_issues",
		Description: "Search issues with filters",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId": StringParam("Project ID"),
			"state":     StringParam("Filter by state"),
			"search":    StringParam("Full text search"),
			"assigneeId": StringParam("Filter by assignee agent ID"),
			"limit":     StringParam("Max results (default 20)"),
			"offset":    StringParam("Offset for pagination"),
		}, nil),
		Handler: h.handleSearchIssues,
	})

	registry.Register(&ToolDefinition{
		Name:        "list_agents",
		Description: "List registered agents",
		InputSchema: ObjectSchema(map[string]interface{}{
			"kind":      StringParam("Filter by kind: ai / human / hybrid"),
			"status":    StringParam("Filter by status: online / busy / offline / error"),
			"projectId": StringParam("Filter by project membership"),
		}, nil),
		Handler: h.handleListAgents,
	})

	registry.Register(&ToolDefinition{
		Name:        "agent_heartbeat",
		Description: "Update agent heartbeat timestamp",
		InputSchema: ObjectSchema(map[string]interface{}{
			"agentId": StringRequiredParam("Agent ID"),
		}, []string{"agentId"}),
		Handler: h.handleHeartbeat,
	})

	registry.Register(&ToolDefinition{
		Name:        "check_notifications",
		Description: "Check notifications for an agent",
		InputSchema: ObjectSchema(map[string]interface{}{
			"agentId": StringRequiredParam("Agent ID"),
		}, []string{"agentId"}),
		Handler: h.handleCheckNotifications,
	})

	registry.Register(&ToolDefinition{
		Name:        "submit_feedback",
		Description: "Submit feedback for an issue, comment, agent, or assignment",
		InputSchema: ObjectSchema(map[string]interface{}{
			"targetType": StringRequiredParam("Target type: issue / comment / agent / assignment"),
			"targetId":   StringRequiredParam("Target ID"),
			"authorId":   StringRequiredParam("Author agent ID"),
			"rating":     StringRequiredParam("Rating 1-5"),
			"body":       StringParam("Feedback body text"),
		}, []string{"targetType", "targetId", "authorId", "rating"}),
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

	registry.Register(&ToolDefinition{
		Name:        "list_skills",
		Description: "List available skills for a project",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId": StringRequiredParam("Project ID"),
		}, []string{"projectId"}),
		Handler: h.handleListSkills,
	})

	registry.Register(&ToolDefinition{
		Name:        "run_skill",
		Description: "Execute a skill and return its definition",
		InputSchema: ObjectSchema(map[string]interface{}{
			"skillId": StringRequiredParam("Skill ID"),
		}, []string{"skillId"}),
		Handler: h.handleRunSkill,
	})

	registry.Register(&ToolDefinition{
		Name:        "create_skill",
		Description: "Create a new skill definition for a project",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId":   StringRequiredParam("Project ID"),
			"name":        StringRequiredParam("Skill name"),
			"description": StringRequiredParam("Skill description"),
			"definition":  StringRequiredParam("Skill YAML definition"),
		}, []string{"projectId", "name", "description", "definition"}),
		Handler: h.handleCreateSkill,
	})
}

// ─── Handler Implementations ───────────────────────────────

func (h *Handlers) handleCreateProject(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	project, err := h.projectSvc.Create(p.Name, p.Description)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":   fmt.Sprintf("%d", project.ID),
		"name": project.Name,
	})
}

func (h *Handlers) handleRegisterAgent(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		Name           string   `json:"name"`
		Kind           string   `json:"kind"`
		ExternalID     string   `json:"externalId"`
		Secret         string   `json:"secret"`
		Capabilities   []string `json:"capabilities"`
		BootstrapToken string   `json:"bootstrapToken"`
		DeviceInfo     string   `json:"deviceInfo"`
		ModelInfo      string   `json:"modelInfo"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	kind := models.AgentKind(p.Kind)
	if kind == "" {
		kind = models.AgentKindAI
	}

	// AI agent registration requires bootstrap token unless an AI already exists
	if kind == models.AgentKindAI && h.auth != nil {
		count, err := h.agentSvc.CountByKind(models.AgentKindAI)
		if err != nil {
			return NewInternalError(id, err.Error())
		}
		if count == 0 {
			if !h.auth.UseBootstrapToken(p.BootstrapToken) {
				return NewError(id, -32002, "Invalid or already used bootstrap token")
			}
		}
	}

	agent, err := h.agentSvc.Register(p.Name, kind, p.ExternalID, p.Secret, p.Capabilities, p.DeviceInfo, p.ModelInfo)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":         fmt.Sprintf("%d", agent.ID),
		"name":       agent.Name,
		"kind":       string(agent.Kind),
		"externalId": agent.ExternalID,
	})
}

func (h *Handlers) handleLoginAgent(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		ExternalID string `json:"externalId"`
		Secret     string `json:"secret"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	result, err := h.agentSvc.Login(p.ExternalID, p.Secret)
	if err != nil {
		return NewError(id, -32001, "Authentication failed: "+err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":     fmt.Sprintf("%d", result.Agent.ID),
		"name":   result.Agent.Name,
		"kind":   string(result.Agent.Kind),
		"status": string(result.Agent.Status),
		"token":  result.Token,
	})
}

func (h *Handlers) handleCreateIssue(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		ProjectID   string   `json:"projectId"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		CreatorID   string   `json:"creatorId"`
		Priority    string   `json:"priority"`
		AssigneeIDs []string `json:"assigneeIds"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	if p.Title == "" || p.ProjectID == "" || p.CreatorID == "" {
		return NewError(id, -32602, "Missing required params: projectId, title, creatorId")
	}

	projectID, _ := strconv.ParseUint(p.ProjectID, 10, 64)
	creatorID, _ := strconv.ParseUint(p.CreatorID, 10, 64)
	priority := models.PriorityMedium
	if p.Priority != "" {
		priority = models.Priority(p.Priority)
	}

	var assigneeIDs []uint
	for _, a := range p.AssigneeIDs {
		if aid, err := strconv.ParseUint(a, 10, 64); err == nil {
			assigneeIDs = append(assigneeIDs, uint(aid))
		}
	}

	issue, err := h.issueSvc.Create(uint(projectID), uint(creatorID), p.Title, p.Description, priority, assigneeIDs, nil)
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

func (h *Handlers) handleAddComment(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		IssueID  string `json:"issueId"`
		AuthorID string `json:"authorId"`
		Body     string `json:"body"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	issueID, _ := strconv.ParseUint(p.IssueID, 10, 64)
	authorID, _ := strconv.ParseUint(p.AuthorID, 10, 64)

	comment, err := h.commentSvc.Create(uint(issueID), uint(authorID), p.Body, models.CommentMarkdown, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":   fmt.Sprintf("%d", comment.ID),
		"body": comment.Body,
	})
}

func (h *Handlers) handleAssignIssue(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		IssueID string `json:"issueId"`
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	issueID, _ := strconv.ParseUint(p.IssueID, 10, 64)
	agentID, _ := strconv.ParseUint(p.AgentID, 10, 64)

	_, err := h.issueSvc.AddAssignee(uint(issueID), uint(agentID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleTransitionIssue(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		IssueID string `json:"issueId"`
		ToState string `json:"toState"`
		ActorID string `json:"actorId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	issueID, _ := strconv.ParseUint(p.IssueID, 10, 64)
	actorID, _ := strconv.ParseUint(p.ActorID, 10, 64)

	issue, err := h.workflowSvc.Transition(uint(issueID), models.IssueState(p.ToState), uint(actorID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", issue.ID),
		"state": string(issue.State),
	})
}

func (h *Handlers) handleSearchIssues(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		ProjectID  string `json:"projectId"`
		State      string `json:"state"`
		Search     string `json:"search"`
		AssigneeID string `json:"assigneeId"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
	}
	json.Unmarshal(params, &p) // nolint: errcheck

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
		pid, _ := strconv.ParseUint(p.ProjectID, 10, 64)
		v := uint(pid)
		filter.ProjectID = &v
	}
	if p.AssigneeID != "" {
		aid, _ := strconv.ParseUint(p.AssigneeID, 10, 64)
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

func (h *Handlers) handleListAgents(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		ProjectID string `json:"projectId"`
	}
	json.Unmarshal(params, &p) // nolint: errcheck

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
		pid, _ := strconv.ParseUint(p.ProjectID, 10, 64)
		v := uint(pid)
		filter.ProjectID = &v
	}

	agents, err := h.agentSvc.List(filter)
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	items := make([]map[string]interface{}, len(agents))
	for i, a := range agents {
		items[i] = map[string]interface{}{
			"id":     fmt.Sprintf("%d", a.ID),
			"name":   a.Name,
			"kind":   string(a.Kind),
			"status": string(a.Status),
		}
	}
	return NewResponse(id, map[string]interface{}{"items": items})
}

func (h *Handlers) handleHeartbeat(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	aid, _ := strconv.ParseUint(p.AgentID, 10, 64)
	if err := h.agentSvc.Heartbeat(uint(aid)); err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleCheckNotifications(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	aid, _ := strconv.ParseUint(p.AgentID, 10, 64)

	if h.notifSvc == nil {
		return NewResponse(id, map[string]interface{}{"notifications": []interface{}{}})
	}

	notifs := h.notifSvc.ListByAgent(uint(aid))
	items := make([]map[string]interface{}, len(notifs))
	for i, n := range notifs {
		items[i] = map[string]interface{}{
			"id":        n.ID,
			"type":      string(n.Type),
			"issueId":   n.IssueID,
			"message":   n.Message,
			"read":      n.Read,
			"createdAt": n.CreatedAt,
		}
	}
	return NewResponse(id, map[string]interface{}{"notifications": items})
}

func (h *Handlers) handleSubmitFeedback(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
		AuthorID   string `json:"authorId"`
		Rating     string `json:"rating"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	targetID, _ := strconv.ParseUint(p.TargetID, 10, 64)
	authorID, _ := strconv.ParseUint(p.AuthorID, 10, 64)
	rating, _ := strconv.Atoi(p.Rating)

	feedback, err := h.feedbackSvc.Create(
		models.FeedbackTargetType(p.TargetType),
		uint(targetID),
		uint(authorID),
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

func (h *Handlers) handleListFeedback(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	targetID, _ := strconv.ParseUint(p.TargetID, 10, 64)

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

func (h *Handlers) handleCreateSkill(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		ProjectID   string `json:"projectId"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Definition  string `json:"definition"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	projectID, err := strconv.ParseUint(p.ProjectID, 10, 64)
	if err != nil || projectID == 0 {
		return NewError(id, -32602, "Invalid projectId")
	}
	if p.Name == "" || p.Definition == "" {
		return NewError(id, -32602, "Missing required params: name, definition")
	}
	skill, err := h.skillSvc.Create(uint(projectID), p.Name, p.Description, p.Definition)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":          fmt.Sprintf("%d", skill.ID),
		"name":        skill.Name,
		"description": skill.Description,
	})
}

func (h *Handlers) handleListSkills(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	projectID, _ := strconv.ParseUint(p.ProjectID, 10, 64)

	skills, err := h.skillSvc.ListByProject(uint(projectID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	result := make([]map[string]interface{}, len(skills))
	for i, s := range skills {
		result[i] = map[string]interface{}{
			"id":          fmt.Sprintf("%d", s.ID),
			"name":        s.Name,
			"description": s.Description,
		}
	}
	return NewResponse(id, map[string]interface{}{"items": result})
}

func (h *Handlers) handleRunSkill(id json.RawMessage, params json.RawMessage) Response {
	var p struct {
		SkillID string `json:"skillId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	skillID, _ := strconv.ParseUint(p.SkillID, 10, 64)

	skill, err := h.skillSvc.GetByID(uint(skillID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	return NewResponse(id, map[string]interface{}{
		"id":          fmt.Sprintf("%d", skill.ID),
		"name":        skill.Name,
		"description": skill.Description,
		"definition":  skill.Definition,
	})
}
