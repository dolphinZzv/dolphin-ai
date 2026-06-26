package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"chick/internal/models"
	"chick/internal/notifications"
	"chick/internal/service"
)

type Handlers struct {
	projectSvc                  *service.ProjectService
	agentSvc                    *service.AgentService
	issueSvc                    *service.IssueService
	commentSvc                  *service.CommentService
	proposalSvc                 *service.ProposalService
	taskSvc                     *service.TaskService
	workflowSvc                 *service.WorkflowService
	feedbackSvc                 *service.FeedbackService
	notifSvc                    *notifications.Service
	defaultRequirementProjectID uint
}

func NewHandlers(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	proposalSvc *service.ProposalService,
	taskSvc *service.TaskService,
	workflowSvc *service.WorkflowService,
	feedbackSvc *service.FeedbackService,
	notifSvc *notifications.Service,
	defaultRequirementProjectID uint,
) *Handlers {
	return &Handlers{
		projectSvc:                  projectSvc,
		agentSvc:                    agentSvc,
		issueSvc:                    issueSvc,
		commentSvc:                  commentSvc,
		proposalSvc:                 proposalSvc,
		taskSvc:                     taskSvc,
		workflowSvc:                 workflowSvc,
		feedbackSvc:                 feedbackSvc,
		notifSvc:                    notifSvc,
		defaultRequirementProjectID: defaultRequirementProjectID,
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
			"environment": StringParam("Environment name, e.g. staging, production"),
			"branch":      StringParam("Branch name"),
			"link":        StringParam("Related links (one per line for multiple)"),
			"difficulty":  NumberParam("Implementation difficulty (1-5)"),
			"startedAt":   StringParam("Start processing time (RFC3339)"),
			"completedAt": StringParam("End processing time (RFC3339)"),
			"projectId":   StringParam("Project ID (required if member of multiple projects)"),
		}, []string{"title"}),
		Handler: h.handleCreateIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "create_issues_batch",
		Description: "Batch create multiple issues at once",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId": StringParam("Project ID (required if member of multiple projects)"),
			"issues": map[string]interface{}{
				"type":        "array",
				"description": "Array of issues to create",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":       StringRequiredParam("Issue title"),
						"description": StringParam("Issue description in Markdown"),
						"priority":    StringParam("Priority: critical / high / medium / low"),
						"assigneeIds": ArrayParam("Agent IDs to assign", "string"),
						"environment": StringParam("Environment name, e.g. staging, production"),
						"branch":      StringParam("Branch name"),
						"link":        StringParam("Related links (one per line for multiple)"),
					},
					"required": []string{"title"},
				},
			},
		}, []string{"issues"}),
		Handler: h.handleCreateIssuesBatch,
	})

	registry.Register(&ToolDefinition{
		Name:        "edit_issue",
		Description: "Edit an existing issue's title, description, or priority",
		InputSchema: ObjectSchema(map[string]interface{}{
			"issueId":     StringRequiredParam("Issue ID"),
			"title":       StringParam("New issue title"),
			"description": StringParam("New issue description in Markdown"),
			"priority":    StringParam("New priority: critical / high / medium / low"),
			"environment": StringParam("Environment name, e.g. staging, production"),
			"branch":      StringParam("Branch name"),
			"link":        StringParam("Related links (one per line for multiple)"),
			"difficulty":  NumberParam("Implementation difficulty (1-5)"),
			"startedAt":   StringParam("Start processing time (RFC3339)"),
			"completedAt": StringParam("End processing time (RFC3339)"),
		}, []string{"issueId"}),
		Handler: h.handleEditIssue,
	})

	registry.Register(&ToolDefinition{
		Name:        "add_comment",
		Description: "Add a comment to an issue",
		InputSchema: ObjectSchema(map[string]interface{}{
			"issueId": StringRequiredParam("Issue ID"),
			"body":    StringRequiredParam("Comment body (Markdown)"),
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
		InputSchema: ObjectSchema(map[string]interface{}{}, nil),
		Handler:     h.handleHeartbeat,
	})

	registry.Register(&ToolDefinition{
		Name:        "check_notifications",
		Description: "Check notifications for the authenticated agent",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId": StringParam("Optional: filter notifications by project ID"),
		}, nil),
		Handler: h.handleCheckNotifications,
	})

	registry.Register(&ToolDefinition{
		Name:        "mark_notifications_read",
		Description: "Mark notifications as read. Provide notification IDs or omit to mark all as read.",
		InputSchema: ObjectSchema(map[string]interface{}{
			"ids": StringParam("Comma-separated notification IDs to mark as read. If empty, marks all as read."),
		}, nil),
		Handler: h.handleMarkNotificationsRead,
	})

	registry.Register(&ToolDefinition{
		Name:        "get_unread_count",
		Description: "Get the number of unread notifications for the authenticated agent",
		InputSchema: ObjectSchema(map[string]interface{}{}, nil),
		Handler:     h.handleGetUnreadCount,
	})

	registry.Register(&ToolDefinition{
		Name:        "get_notification_settings",
		Description: "Get notification settings for the authenticated agent",
		InputSchema: ObjectSchema(nil, nil),
		Handler:     h.handleGetNotificationSettings,
	})

	registry.Register(&ToolDefinition{
		Name:        "update_notification_setting",
		Description: "Update a notification setting for the authenticated agent",
		InputSchema: ObjectSchema(map[string]interface{}{
			"notificationType": StringRequiredParam("Notification type (e.g. issue_assigned, proposal_created)"),
			"enabled":          StringRequiredParam("Enable or disable: true / false"),
			"channel":          StringParam("Notification channel: in_app (default), email, webhook"),
		}, []string{"notificationType", "enabled"}),
		Handler: h.handleUpdateNotificationSetting,
	})

	registry.Register(&ToolDefinition{
		Name:        "list_notification_types",
		Description: "List all available notification types",
		InputSchema: ObjectSchema(nil, nil),
		Handler:     h.handleListNotificationTypes,
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
	registry.Register(&ToolDefinition{
		Name:        "submit_requirement",
		Description: "Submit a requirement / feature request from an LLM. Creates a requirement record that can be reviewed and turned into issues.",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId":   StringParam("Project ID (required if member of multiple projects)"),
			"title":       StringRequiredParam("Requirement title"),
			"description": StringRequiredParam("Requirement description / details in Markdown"),
		}, []string{"title", "description"}),
		Handler: h.handleSubmitRequirement,
	})
	registry.Register(&ToolDefinition{
		Name:        "create_proposal",
		Description: "Create a new proposal in draft state",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId":   StringParam("Project ID (required if member of multiple projects)"),
			"title":       StringRequiredParam("Proposal title"),
			"description": StringParam("Proposal description in Markdown"),
			"priority":    StringParam("Priority: critical / high / medium / low"),
			"labelIds":    ArrayParam("Label IDs", "string"),
		}, []string{"title"}),
		Handler: h.handleCreateProposal,
	})

	registry.Register(&ToolDefinition{
		Name:        "transition_proposal",
		Description: "Transition a proposal to a new state",
		InputSchema: ObjectSchema(map[string]interface{}{
			"proposalId": StringRequiredParam("Proposal ID"),
			"toState":    StringRequiredParam("Target state: draft / submitted / under_review / approved / rejected / in_execution / completed / cancelled"),
		}, []string{"proposalId", "toState"}),
		Handler: h.handleTransitionProposal,
	})

	registry.Register(&ToolDefinition{
		Name:        "review_proposal",
		Description: "Review a proposal (approve or reject)",
		InputSchema: ObjectSchema(map[string]interface{}{
			"proposalId": StringRequiredParam("Proposal ID"),
			"approved":   StringRequiredParam("true to approve, false to reject"),
			"note":       StringParam("Review note"),
		}, []string{"proposalId", "approved"}),
		Handler: h.handleReviewProposal,
	})

	registry.Register(&ToolDefinition{
		Name:        "add_comment_to_proposal",
		Description: "Add a comment to a proposal",
		InputSchema: ObjectSchema(map[string]interface{}{
			"proposalId": StringRequiredParam("Proposal ID"),
			"body":       StringRequiredParam("Comment body (Markdown)"),
		}, []string{"proposalId", "body"}),
		Handler: h.handleAddCommentToProposal,
	})

	registry.Register(&ToolDefinition{
		Name:        "search_proposals",
		Description: "Search proposals with filters",
		InputSchema: ObjectSchema(map[string]interface{}{
			"projectId": StringParam("Project ID (required if member of multiple projects)"),
			"state":     StringParam("Filter by state: draft / submitted / under_review / approved / rejected / in_execution / completed / cancelled"),
			"search":    StringParam("Full text search"),
			"limit":     NumberParam("Max results (default 20)"),
			"offset":    NumberParam("Offset for pagination"),
		}, nil),
		Handler: h.handleSearchProposals,
	})

	registry.Register(&ToolDefinition{
		Name:        "create_task",
		Description: "Create a task under a proposal",
		InputSchema: ObjectSchema(map[string]interface{}{
			"proposalId":  StringRequiredParam("Proposal ID"),
			"title":       StringRequiredParam("Task title"),
			"description": StringParam("Task description in Markdown"),
			"priority":    StringParam("Priority: critical / high / medium / low"),
			"assigneeId":  StringParam("Agent ID to assign"),
		}, []string{"proposalId", "title"}),
		Handler: h.handleCreateTask,
	})

	registry.Register(&ToolDefinition{
		Name:        "transition_task",
		Description: "Transition a task to a new state",
		InputSchema: ObjectSchema(map[string]interface{}{
			"taskId":  StringRequiredParam("Task ID"),
			"toState": StringRequiredParam("Target state: pending / in_progress / completed / blocked / cancelled"),
		}, []string{"taskId", "toState"}),
		Handler: h.handleTransitionTask,
	})

	registry.Register(&ToolDefinition{
		Name:        "assign_task",
		Description: "Assign an agent to a task",
		InputSchema: ObjectSchema(map[string]interface{}{
			"taskId":     StringRequiredParam("Task ID"),
			"assigneeId": StringRequiredParam("Agent ID to assign"),
		}, []string{"taskId", "assigneeId"}),
		Handler: h.handleAssignTask,
	})

	registry.Register(&ToolDefinition{
		Name:        "link_issues_to_task",
		Description: "Link issues to a task",
		InputSchema: ObjectSchema(map[string]interface{}{
			"taskId":   StringRequiredParam("Task ID"),
			"issueIds": ArrayParam("Issue IDs to link", "string"),
		}, []string{"taskId", "issueIds"}),
		Handler: h.handleLinkIssuesToTask,
	})

	registry.Register(&ToolDefinition{
		Name:        "unlink_issue_from_task",
		Description: "Unlink an issue from a task",
		InputSchema: ObjectSchema(map[string]interface{}{
			"taskId":  StringRequiredParam("Task ID"),
			"issueId": StringRequiredParam("Issue ID to unlink"),
		}, []string{"taskId", "issueId"}),
		Handler: h.handleUnlinkIssueFromTask,
	})

	registry.Register(&ToolDefinition{
		Name:        "add_comment_to_task",
		Description: "Add a comment to a task",
		InputSchema: ObjectSchema(map[string]interface{}{
			"taskId": StringRequiredParam("Task ID"),
			"body":   StringRequiredParam("Comment body (Markdown)"),
		}, []string{"taskId", "body"}),
		Handler: h.handleAddCommentToTask,
	})

	registry.Register(&ToolDefinition{
		Name:        "search_tasks",
		Description: "Search tasks with filters",
		InputSchema: ObjectSchema(map[string]interface{}{
			"proposalId": StringParam("Filter by proposal ID"),
			"state":      StringParam("Filter by state: pending / in_progress / completed / blocked / cancelled"),
			"assigneeId": StringParam("Filter by assignee agent ID"),
			"limit":      NumberParam("Max results (default 20)"),
			"offset":     NumberParam("Offset for pagination"),
		}, nil),
		Handler: h.handleSearchTasks,
	})

}

// ─── Handler Implementations ───────────────────────────────

// ─── Proposal MCP Handlers ──────────────────────────────────

func (h *Handlers) handleCreateProposal(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		ProjectID   string   `json:"projectId"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    string   `json:"priority"`
		LabelIDs    []string `json:"labelIds"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if p.Title == "" {
		return NewError(id, -32602, "Missing required param: title")
	}
	projectID, err := h.resolveProject(p.ProjectID, authorID)
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
	var labelIDs []uint
	for _, lid := range p.LabelIDs {
		if id, err := strconv.ParseUint(lid, 10, 64); err == nil {
			labelIDs = append(labelIDs, uint(id))
		}
	}
	proposal, err := h.proposalSvc.Create(projectID, authorID, p.Title, p.Description, priority, labelIDs)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":        fmt.Sprintf("%d", proposal.ID),
		"number":    proposal.Number,
		"title":     proposal.Title,
		"state":     string(proposal.State),
		"priority":  string(proposal.Priority),
		"projectId": fmt.Sprintf("%d", projectID),
	})
}

func (h *Handlers) handleTransitionProposal(id json.RawMessage, params json.RawMessage, actorID uint, remoteAddr string) Response {
	var p struct {
		ProposalID string `json:"proposalId"`
		ToState    string `json:"toState"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if actorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	proposalID, err := strconv.ParseUint(p.ProposalID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid proposalId: "+p.ProposalID)
	}
	updated, err := h.proposalSvc.TransitionState(uint(proposalID), models.ProposalState(p.ToState), actorID, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", updated.ID),
		"state": string(updated.State),
	})
}

func (h *Handlers) handleReviewProposal(id json.RawMessage, params json.RawMessage, reviewerID uint, remoteAddr string) Response {
	var p struct {
		ProposalID string `json:"proposalId"`
		Approved   string `json:"approved"`
		Note       string `json:"note"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if reviewerID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	proposalID, err := strconv.ParseUint(p.ProposalID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid proposalId: "+p.ProposalID)
	}
	approved := p.Approved == "true"
	var note *string
	if p.Note != "" {
		note = &p.Note
	}
	updated, err := h.proposalSvc.Review(uint(proposalID), reviewerID, approved, note)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", updated.ID),
		"state": string(updated.State),
	})
}

func (h *Handlers) handleAddCommentToProposal(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		ProposalID string `json:"proposalId"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	proposalID, err := strconv.ParseUint(p.ProposalID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid proposalId: "+p.ProposalID)
	}
	comment, err := h.commentSvc.CreateForProposal(uint(proposalID), authorID, p.Body, models.CommentMarkdown, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":   fmt.Sprintf("%d", comment.ID),
		"body": comment.Body,
	})
}

func (h *Handlers) handleSearchProposals(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		ProjectID string `json:"projectId"`
		State     string `json:"state"`
		Search    string `json:"search"`
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	projectID, err := h.resolveProject(p.ProjectID, agentID)
	if err != nil {
		return NewError(id, -32602, err.Error())
	}
	filter := models.ProposalFilter{
		ProjectID: &projectID,
		Search:    p.Search,
		Limit:     p.Limit,
		Offset:    p.Offset,
	}
	if p.Limit <= 0 {
		filter.Limit = 20
	}
	if p.State != "" {
		filter.State = []models.ProposalState{models.ProposalState(p.State)}
	}
	proposals, total, err := h.proposalSvc.List(filter)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	items := make([]map[string]interface{}, len(proposals))
	for i, prop := range proposals {
		items[i] = map[string]interface{}{
			"id":     fmt.Sprintf("%d", prop.ID),
			"number": prop.Number,
			"title":  prop.Title,
			"state":  string(prop.State),
		}
	}
	return NewResponse(id, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

// ─── Task MCP Handlers ──────────────────────────────────────

func (h *Handlers) handleCreateTask(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		ProposalID  string `json:"proposalId"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		AssigneeID  string `json:"assigneeId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if p.Title == "" {
		return NewError(id, -32602, "Missing required param: title")
	}
	proposalID, err := strconv.ParseUint(p.ProposalID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid proposalId: "+p.ProposalID)
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
	var assigneeID *uint
	if p.AssigneeID != "" {
		aid, err := strconv.ParseUint(p.AssigneeID, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid assigneeId: "+p.AssigneeID)
		}
		v := uint(aid)
		assigneeID = &v
	}
	// Get proposal for projectID
	proposal, err := h.proposalSvc.GetByID(uint(proposalID))
	if err != nil {
		return NewInternalError(id, "proposal not found: "+err.Error())
	}
	task, err := h.taskSvc.Create(uint(proposalID), proposal.ProjectID, authorID, p.Title, p.Description, priority, assigneeID)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":         fmt.Sprintf("%d", task.ID),
		"number":     task.Number,
		"title":      task.Title,
		"state":      string(task.State),
		"priority":   string(task.Priority),
		"proposalId": fmt.Sprintf("%d", proposalID),
	})
}

func (h *Handlers) handleTransitionTask(id json.RawMessage, params json.RawMessage, actorID uint, remoteAddr string) Response {
	var p struct {
		TaskID  string `json:"taskId"`
		ToState string `json:"toState"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if actorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	taskID, err := strconv.ParseUint(p.TaskID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid taskId: "+p.TaskID)
	}
	updated, err := h.taskSvc.TransitionState(uint(taskID), models.TaskState(p.ToState), actorID)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", updated.ID),
		"state": string(updated.State),
	})
}

func (h *Handlers) handleAssignTask(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		TaskID     string `json:"taskId"`
		AssigneeID string `json:"assigneeId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	taskID, err := strconv.ParseUint(p.TaskID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid taskId: "+p.TaskID)
	}
	assigneeID, err := strconv.ParseUint(p.AssigneeID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid assigneeId: "+p.AssigneeID)
	}
	updated, err := h.taskSvc.Assign(uint(taskID), uint(assigneeID))
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", updated.ID),
		"state": string(updated.State),
	})
}

func (h *Handlers) handleLinkIssuesToTask(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		TaskID   string   `json:"taskId"`
		IssueIDs []string `json:"issueIds"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	taskID, err := strconv.ParseUint(p.TaskID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid taskId: "+p.TaskID)
	}
	for _, iid := range p.IssueIDs {
		issueID, err := strconv.ParseUint(iid, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid issueId: "+iid)
		}
		if err := h.taskSvc.LinkIssue(uint(taskID), uint(issueID)); err != nil {
			return NewInternalError(id, err.Error())
		}
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleUnlinkIssueFromTask(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		TaskID  string `json:"taskId"`
		IssueID string `json:"issueId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	taskID, err := strconv.ParseUint(p.TaskID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid taskId: "+p.TaskID)
	}
	issueID, err := strconv.ParseUint(p.IssueID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid issueId: "+p.IssueID)
	}
	if err := h.taskSvc.UnlinkIssue(uint(taskID), uint(issueID)); err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleAddCommentToTask(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		TaskID string `json:"taskId"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	taskID, err := strconv.ParseUint(p.TaskID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid taskId: "+p.TaskID)
	}
	comment, err := h.commentSvc.CreateForTask(uint(taskID), authorID, p.Body, models.CommentMarkdown, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":   fmt.Sprintf("%d", comment.ID),
		"body": comment.Body,
	})
}

func (h *Handlers) handleSearchTasks(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		ProposalID string `json:"proposalId"`
		State      string `json:"state"`
		Search     string `json:"search"`
		AssigneeID string `json:"assigneeId"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	filter := models.TaskFilter{
		Search: p.Search,
		Limit:  p.Limit,
		Offset: p.Offset,
	}
	if p.Limit <= 0 {
		filter.Limit = 20
	}
	if p.State != "" {
		filter.State = []models.TaskState{models.TaskState(p.State)}
	}
	if p.ProposalID != "" {
		pid, err := strconv.ParseUint(p.ProposalID, 10, 64)
		if err == nil {
			v := uint(pid)
			filter.ProposalID = &v
		}
	}
	if p.AssigneeID != "" {
		aid, err := strconv.ParseUint(p.AssigneeID, 10, 64)
		if err == nil {
			v := uint(aid)
			filter.AssigneeID = &v
		}
	}
	tasks, total, err := h.taskSvc.List(filter)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	items := make([]map[string]interface{}, len(tasks))
	for i, t := range tasks {
		items[i] = map[string]interface{}{
			"id":     fmt.Sprintf("%d", t.ID),
			"number": t.Number,
			"title":  t.Title,
			"state":  string(t.State),
		}
	}
	return NewResponse(id, map[string]interface{}{
		"items": items,
		"total": total,
	})
}

func (h *Handlers) handleGetAgentInfo(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	var p struct {
		AgentID    string `json:"agentId"`
		ExternalID string `json:"externalId"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}

	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}

	var targetID uint
	if p.AgentID != "" {
		aid, err := strconv.ParseUint(p.AgentID, 10, 64)
		if err != nil {
			return NewError(id, -32602, "Invalid agentId: "+p.AgentID)
		}
		targetID = uint(aid)
	} else if p.ExternalID != "" {
		agent, err := h.agentSvc.GetByExternalID(p.ExternalID)
		if err != nil {
			return NewInternalError(id, err.Error())
		}
		if agent == nil {
			return NewError(id, -32602, "Agent not found")
		}
		targetID = agent.ID
	} else {
		return NewError(id, -32602, "Provide agentId or externalId")
	}

	// Agents can always view themselves; otherwise they must share a project
	if agentID != targetID {
		ok, err := h.projectSvc.CheckSharedProject(agentID, targetID)
		if err != nil || !ok {
			return NewError(id, -32602, "Access denied: agent not found or not in same project")
		}
	}

	agent, err := h.agentSvc.GetByID(targetID)
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

func (h *Handlers) handleEditIssue(id json.RawMessage, params json.RawMessage, actorID uint, remoteAddr string) Response {
	var p struct {
		IssueID     string `json:"issueId"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    string `json:"priority"`
		Environment string `json:"environment"`
		Branch      string `json:"branch"`
		Link        string `json:"link"`
		Difficulty  int    `json:"difficulty"`
		StartedAt   string `json:"startedAt"`
		CompletedAt string `json:"completedAt"`
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

	// Verify caller is a member of the issue's project
	existing, err := h.issueSvc.GetByID(uint(issueID))
	if err != nil {
		return NewError(id, -32602, "Issue not found")
	}
	if _, err := h.projectSvc.GetMemberRole(existing.ProjectID, actorID); err != nil {
		return NewError(id, -32602, "Access denied: not a member of this project")
	}

	// At least one field must be provided for update
	if p.Title == "" && p.Description == "" && p.Priority == "" && p.Difficulty == 0 && p.StartedAt == "" && p.CompletedAt == "" {
		return NewError(id, -32602, "At least one field must be provided for update")
	}

	priority := models.Priority(p.Priority)
	if p.Priority != "" {
		switch p.Priority {
		case "critical", "high", "medium", "low":
			priority = models.Priority(p.Priority)
		default:
			return NewError(id, -32602, "Invalid priority: must be critical/high/medium/low")
		}
	}

	var diff *int
	if p.Difficulty != 0 {
		if p.Difficulty < 1 || p.Difficulty > 5 {
			return NewError(id, -32602, "Invalid difficulty: must be 1-5")
		}
		d := p.Difficulty
		diff = &d
	}

	var startedAt, completedAt *time.Time
	if p.StartedAt != "" {
		t, err := time.Parse(time.RFC3339, p.StartedAt)
		if err != nil {
			return NewError(id, -32602, "Invalid startedAt: must be RFC3339 format")
		}
		startedAt = &t
	}
	if p.CompletedAt != "" {
		t, err := time.Parse(time.RFC3339, p.CompletedAt)
		if err != nil {
			return NewError(id, -32602, "Invalid completedAt: must be RFC3339 format")
		}
		completedAt = &t
	}

	issue, err := h.issueSvc.Update(uint(issueID), p.Title, p.Description, priority, nil, nil, strPtr(p.Environment), strPtr(p.Branch), strPtr(p.Link), startedAt, completedAt, diff)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":          fmt.Sprintf("%d", issue.ID),
		"number":      issue.Number,
		"title":       issue.Title,
		"description": issue.Description,
		"state":       string(issue.State),
		"priority":    string(issue.Priority),
		"environment": nilStr(issue.Environment),
		"branch":      nilStr(issue.Branch),
		"link":        nilStr(issue.Link),
	})
}

func (h *Handlers) handleCreateIssue(id json.RawMessage, params json.RawMessage, creatorID uint, remoteAddr string) Response {
	var p struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Priority    string   `json:"priority"`
		AssigneeIDs []string `json:"assigneeIds"`
		MilestoneID string   `json:"milestoneId"`
		Environment string   `json:"environment"`
		Branch      string   `json:"branch"`
		Link        string   `json:"link"`
		Difficulty  int      `json:"difficulty"`
		StartedAt   string   `json:"startedAt"`
		CompletedAt string   `json:"completedAt"`
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
	var diff *int
	if p.Difficulty != 0 {
		if p.Difficulty < 1 || p.Difficulty > 5 {
			return NewError(id, -32602, "Invalid difficulty: must be 1-5")
		}
		d := p.Difficulty
		diff = &d
	}

	var startedAt, completedAt *time.Time
	if p.StartedAt != "" {
		t, err := time.Parse(time.RFC3339, p.StartedAt)
		if err != nil {
			return NewError(id, -32602, "Invalid startedAt: must be RFC3339 format")
		}
		startedAt = &t
	}
	if p.CompletedAt != "" {
		t, err := time.Parse(time.RFC3339, p.CompletedAt)
		if err != nil {
			return NewError(id, -32602, "Invalid completedAt: must be RFC3339 format")
		}
		completedAt = &t
	}
	env, branch, link := strPtr(p.Environment), strPtr(p.Branch), strPtr(p.Link)
	issue, err := h.issueSvc.Create(projectID, creatorID, p.Title, p.Description, priority, assigneeIDs, nil, milestoneID, env, branch, link, diff, startedAt, completedAt)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":          fmt.Sprintf("%d", issue.ID),
		"number":      issue.Number,
		"title":       issue.Title,
		"state":       string(issue.State),
		"environment": nilStr(issue.Environment),
		"branch":      nilStr(issue.Branch),
		"link":        nilStr(issue.Link),
	})
}

func (h *Handlers) handleCreateIssuesBatch(id json.RawMessage, params json.RawMessage, creatorID uint, remoteAddr string) Response {
	var p struct {
		ProjectID string            `json:"projectId"`
		Issues    []json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if creatorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if len(p.Issues) == 0 {
		return NewError(id, -32602, "Missing required param: issues (must be a non-empty array)")
	}

	projectID, err := h.resolveProject(p.ProjectID, creatorID)
	if err != nil {
		return NewError(id, -32602, err.Error())
	}

	var results []map[string]interface{}
	for i, raw := range p.Issues {
		var issue struct {
			Title       string   `json:"title"`
			Description string   `json:"description"`
			Priority    string   `json:"priority"`
			AssigneeIDs []string `json:"assigneeIds"`
			Environment string   `json:"environment"`
			Branch      string   `json:"branch"`
			Link        string   `json:"link"`
		}
		if err := json.Unmarshal(raw, &issue); err != nil {
			return NewError(id, -32602, fmt.Sprintf("issues[%d]: invalid params: %s", i, err))
		}
		if issue.Title == "" {
			return NewError(id, -32602, fmt.Sprintf("issues[%d]: missing required param: title", i))
		}

		priority := models.PriorityMedium
		if issue.Priority != "" {
			switch issue.Priority {
			case "critical", "high", "medium", "low":
				priority = models.Priority(issue.Priority)
			default:
				return NewError(id, -32602, fmt.Sprintf("issues[%d]: invalid priority: must be critical/high/medium/low", i))
			}
		}

		var assigneeIDs []uint
		for _, a := range issue.AssigneeIDs {
			if aid, err := strconv.ParseUint(a, 10, 64); err == nil {
				assigneeIDs = append(assigneeIDs, uint(aid))
			}
		}

		created, err := h.issueSvc.Create(projectID, creatorID, issue.Title, issue.Description, priority, assigneeIDs, nil, nil, strPtr(issue.Environment), strPtr(issue.Branch), strPtr(issue.Link), nil, nil, nil)
		if err != nil {
			return NewInternalError(id, fmt.Sprintf("issues[%d]: %s", i, err.Error()))
		}
		results = append(results, map[string]interface{}{
			"id":     fmt.Sprintf("%d", created.ID),
			"number": created.Number,
			"title":  created.Title,
			"state":  string(created.State),
		})
	}

	return NewResponse(id, map[string]interface{}{
		"items": results,
		"total": len(results),
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

	// Verify caller is a member of the issue's project
	issue, err := h.issueSvc.GetByID(uint(issueID))
	if err != nil {
		return NewError(id, -32602, "Issue not found")
	}
	if _, err := h.projectSvc.GetMemberRole(issue.ProjectID, authorID); err != nil {
		return NewError(id, -32602, "Access denied: not a member of this project")
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
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	issueID, err := strconv.ParseUint(p.IssueID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid issueId: "+p.IssueID)
	}
	assignAgentID, err := strconv.ParseUint(p.AgentID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid agentId: "+p.AgentID)
	}

	// Verify caller is a member of the issue's project
	issue, err := h.issueSvc.GetByID(uint(issueID))
	if err != nil {
		return NewError(id, -32602, "Issue not found")
	}
	if _, err := h.projectSvc.GetMemberRole(issue.ProjectID, agentID); err != nil {
		return NewError(id, -32602, "Access denied: not a member of this project")
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

	// Verify caller is a member of the issue's project
	issue, err := h.issueSvc.GetByID(uint(issueID))
	if err != nil {
		return NewError(id, -32602, "Issue not found")
	}
	if _, err := h.projectSvc.GetMemberRole(issue.ProjectID, actorID); err != nil {
		return NewError(id, -32602, "Access denied: not a member of this project")
	}

	updated, err := h.workflowSvc.Transition(uint(issueID), models.IssueState(p.ToState), actorID, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{
		"id":    fmt.Sprintf("%d", updated.ID),
		"state": string(updated.State),
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

	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
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
		// Verify caller is a member of the specified project
		if _, err := h.projectSvc.GetMemberRole(v, agentID); err != nil {
			return NewError(id, -32602, "Access denied: not a member of this project")
		}
		filter.ProjectID = &v
	} else {
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
			"id":         n.ID,
			"type":       string(n.Type),
			"issueId":    n.IssueID,
			"commentId":  n.CommentID,
			"proposalId": n.ProposalID,
			"taskId":     n.TaskID,
			"projectId":  n.ProjectID,
			"message":    n.Message,
			"read":       n.Read,
			"createdAt":  n.CreatedAt,
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
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}

	targetID, err := strconv.ParseUint(p.TargetID, 10, 64)
	if err != nil {
		return NewError(id, -32602, "Invalid targetId: "+p.TargetID)
	}

	// Verify caller has access to the target
	switch models.FeedbackTargetType(p.TargetType) {
	case models.FeedbackTargetIssue:
		issue, err := h.issueSvc.GetByID(uint(targetID))
		if err != nil {
			return NewError(id, -32602, "Issue not found")
		}
		if _, err := h.projectSvc.GetMemberRole(issue.ProjectID, agentID); err != nil {
			return NewError(id, -32602, "Access denied: not a member of this project")
		}
	case models.FeedbackTargetAgent:
		if agentID != uint(targetID) {
			ok, err := h.projectSvc.CheckSharedProject(agentID, uint(targetID))
			if err != nil || !ok {
				return NewError(id, -32602, "Access denied: agent not found or not in same project")
			}
		}
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

// resolveRequirementProject returns the project ID for submitting requirements.
// Returns an error if no requirement project is configured or it doesn't exist.
func (h *Handlers) resolveRequirementProject(agentID uint) (uint, error) {
	if h.defaultRequirementProjectID == 0 {
		return 0, fmt.Errorf("requirement submission is not supported (no CHICK_REQUIREMENT_PROJECT_ID configured)")
	}
	proj, err := h.projectSvc.GetByID(h.defaultRequirementProjectID)
	if err != nil {
		return 0, fmt.Errorf("requirement project %d not found: %w", h.defaultRequirementProjectID, err)
	}
	return proj.ID, nil
}

func (h *Handlers) handleSubmitRequirement(id json.RawMessage, params json.RawMessage, authorID uint, remoteAddr string) Response {
	var p struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if authorID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if p.Title == "" {
		return NewError(id, -32602, "Missing required param: title")
	}
	if p.Description == "" {
		return NewError(id, -32602, "Missing required param: description")
	}

	// Resolve the requirement project — auto-create if needed
	projectID, err := h.resolveRequirementProject(authorID)
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	issue, err := h.issueSvc.Create(projectID, authorID, p.Title, p.Description, models.PriorityMedium, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		return NewInternalError(id, err.Error())
	}

	return NewResponse(id, map[string]interface{}{
		"id":          fmt.Sprintf("%d", issue.ID),
		"number":      issue.Number,
		"title":       issue.Title,
		"description": issue.Description,
		"state":       string(issue.State),
		"projectId":   fmt.Sprintf("%d", projectID),
	})
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nilInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func nilTimeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func maskToken(token string) string {
	if len(token) <= 10 {
		return token
	}
	return token[:6] + "…" + token[len(token)-4:]
}

func (h *Handlers) handleMarkNotificationsRead(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if h.notifSvc == nil {
		return NewResponse(id, map[string]interface{}{"success": true})
	}

	var p struct {
		IDs string `json:"ids"`
	}
	json.Unmarshal(params, &p)

	if p.IDs == "" {
		if err := h.notifSvc.MarkAllRead(agentID); err != nil {
			return NewInternalError(id, err.Error())
		}
		return NewResponse(id, map[string]interface{}{"success": true, "markedAll": true})
	}

	for _, s := range strings.Split(p.IDs, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		nid, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			continue
		}
		h.notifSvc.MarkRead(uint(nid))
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleGetUnreadCount(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if h.notifSvc == nil {
		return NewResponse(id, map[string]interface{}{"count": 0})
	}
	count := h.notifSvc.UnreadCount(agentID)
	return NewResponse(id, map[string]interface{}{"count": count})
}

func (h *Handlers) handleGetNotificationSettings(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	if h.notifSvc == nil {
		return NewResponse(id, map[string]interface{}{"settings": []interface{}{}})
	}
	settings, err := h.notifSvc.GetSettings(agentID)
	if err != nil {
		return NewInternalError(id, err.Error())
	}
	items := make([]map[string]interface{}, len(settings))
	for i, s := range settings {
		items[i] = map[string]interface{}{
			"id":               s.ID,
			"agentId":          s.AgentID,
			"notificationType": s.NotificationType,
			"enabled":          s.Enabled,
			"channel":          s.Channel,
		}
	}
	return NewResponse(id, map[string]interface{}{"settings": items})
}

func (h *Handlers) handleUpdateNotificationSetting(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	if agentID == 0 {
		return NewError(id, -32602, "Not authenticated")
	}
	var p struct {
		NotificationType string `json:"notificationType"`
		Enabled          string `json:"enabled"`
		Channel          string `json:"channel"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return NewError(id, -32602, "Invalid params: "+err.Error())
	}
	if p.NotificationType == "" {
		return NewError(id, -32602, "notificationType is required")
	}
	enabled := p.Enabled == "true"
	if err := h.notifSvc.UpdateSetting(agentID, p.NotificationType, enabled, p.Channel); err != nil {
		return NewInternalError(id, err.Error())
	}
	return NewResponse(id, map[string]interface{}{"success": true})
}

func (h *Handlers) handleListNotificationTypes(id json.RawMessage, params json.RawMessage, agentID uint, remoteAddr string) Response {
	types := notifications.AllNotificationTypes()
	return NewResponse(id, map[string]interface{}{"types": types})
}
