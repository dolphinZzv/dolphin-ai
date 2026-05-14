package mcp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"chick/internal/models"
	"chick/internal/service"
)

type Resources struct {
	resources []ResourceDefinition
	projectSvc *service.ProjectService
	agentSvc   *service.AgentService
	issueSvc   *service.IssueService
}

func NewResources(projectSvc *service.ProjectService, agentSvc *service.AgentService, issueSvc *service.IssueService) *Resources {
	return &Resources{
		resources: []ResourceDefinition{
			{
				URI:         "project://",
				Name:        "Project",
				Description: "Project details including members and stats",
				MimeType:    "application/json",
			},
			{
				URI:         "issue://",
				Name:        "Issue",
				Description: "Issue details by project and number",
				MimeType:    "application/json",
			},
			{
				URI:         "agent://",
				Name:        "Agent",
				Description: "Agent details",
				MimeType:    "application/json",
			},
		},
		projectSvc: projectSvc,
		agentSvc:   agentSvc,
		issueSvc:   issueSvc,
	}
}

func (r *Resources) List() []ResourceDefinition {
	return r.resources
}

func (r *Resources) Templates() []ResourceTemplate {
	return []ResourceTemplate{
		{
			URITemplate: "project://{id}",
			Name:        "Project",
			Description: "Project details including members and stats",
			MimeType:    "application/json",
		},
		{
			URITemplate: "issue://{projectId}/{issueNumber}",
			Name:        "Issue",
			Description: "Issue details by project and number",
			MimeType:    "application/json",
		},
		{
			URITemplate: "agent://{id}",
			Name:        "Agent",
			Description: "Agent details",
			MimeType:    "application/json",
		},
	}
}

func (r *Resources) Read(uri string) (interface{}, error) {
	switch {
	case strings.HasPrefix(uri, "project://"):
		idStr := strings.TrimPrefix(uri, "project://")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid project id: %s", idStr)
		}
		return r.readProject(uri, uint(id))

	case strings.HasPrefix(uri, "issue://"):
		rest := strings.TrimPrefix(uri, "issue://")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid issue URI: %s (expected issue://{project}/{number})", uri)
		}
		projectID, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid project id: %s", parts[0])
		}
		number, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid issue number: %s", parts[1])
		}
		return r.readIssue(uri, uint(projectID), uint(number))

	case strings.HasPrefix(uri, "agent://"):
		idStr := strings.TrimPrefix(uri, "agent://")
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid agent id: %s", idStr)
		}
		return r.readAgent(uri, uint(id))

	default:
		return nil, fmt.Errorf("unknown resource: %s", uri)
	}
}

func (r *Resources) readProject(uri string, id uint) (interface{}, error) {
	p, err := r.projectSvc.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	members, err := r.projectSvc.ListMembers(id)
	if err != nil {
		members = nil
	}
	memberList := make([]map[string]interface{}, 0, len(members))
	for _, m := range members {
		memberList = append(memberList, map[string]interface{}{
			"agentId": fmt.Sprintf("%d", m.AgentID),
			"role":    string(m.Role),
		})
	}
	data, _ := json.Marshal(map[string]interface{}{
		"id":          fmt.Sprintf("%d", p.ID),
		"name":        p.Name,
		"description": p.Description,
		"members":     memberList,
	})
	return map[string]interface{}{
		"uri":      uri,
		"mimeType": "application/json",
		"text":     string(data),
	}, nil
}

func (r *Resources) readIssue(uri string, projectID uint, number uint) (interface{}, error) {
	issues, _, err := r.issueSvc.List(models.IssueFilter{
		ProjectID: &projectID,
		Limit:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("issue not found: %w", err)
	}
	var found *models.Issue
	for i := range issues {
		if issues[i].Number == number {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("issue #%d not found in project %d", number, projectID)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"id":          fmt.Sprintf("%d", found.ID),
		"number":      found.Number,
		"title":       found.Title,
		"description": found.Description,
		"state":       string(found.State),
		"priority":    string(found.Priority),
	})
	return map[string]interface{}{
		"uri":      uri,
		"mimeType": "application/json",
		"text":     string(data),
	}, nil
}

func (r *Resources) readAgent(uri string, id uint) (interface{}, error) {
	a, err := r.agentSvc.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"id":           fmt.Sprintf("%d", a.ID),
		"number":       a.Number,
		"name":         a.Name,
		"kind":         string(a.Kind),
		"status":       string(a.Status),
		"externalId":   a.ExternalID,
		"capabilities": a.Capabilities,
		"deviceInfo":   a.DeviceInfo,
		"modelInfo":    a.ModelInfo,
		"lastIp":       a.LastIP,
	})
	return map[string]interface{}{
		"uri":      uri,
		"mimeType": "application/json",
		"text":     string(data),
	}, nil
}
