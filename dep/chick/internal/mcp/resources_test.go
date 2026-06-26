package mcp_test

import (
	"encoding/json"
	"strings"
	"testing"

	"chick/internal/mcp"
	"chick/internal/models"
)

func TestResourcesList(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/list",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	resources, ok := result["resources"].([]mcp.ResourceDefinition)
	if !ok {
		t.Fatal("expected resources array of ResourceDefinition")
	}
	if len(resources) != 5 {
		t.Fatalf("expected 5 resources, got %d", len(resources))
	}

	// Check all three resource types are listed
	uris := make(map[string]bool)
	for _, r := range resources {
		uris[r.URI] = true
	}
	if !uris["project://"] {
		t.Error("expected project:// resource")
	}
	if !uris["issue://"] {
		t.Error("expected issue:// resource")
	}
	if !uris["agent://"] {
		t.Error("expected agent:// resource")
	}
}

func TestResourcesTemplatesList(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/templates/list",
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	templates, ok := result["resourceTemplates"].([]mcp.ResourceTemplate)
	if !ok {
		data, _ := json.Marshal(result["resourceTemplates"])
		var templatesList []mcp.ResourceTemplate
		if err := json.Unmarshal(data, &templatesList); err != nil {
			t.Fatalf("expected resourceTemplates array: %v", err)
		}
		templates = templatesList
	}
	if len(templates) != 5 {
		t.Fatalf("expected 5 resource templates, got %d", len(templates))
	}

	tpls := make(map[string]bool)
	for _, t := range templates {
		tpls[t.URITemplate] = true
	}
	if !tpls["project://{id}"] {
		t.Error("expected project://{id} template")
	}
	if !tpls["issue://{projectId}/{issueNumber}"] {
		t.Error("expected issue://{projectId}/{issueNumber} template")
	}
	if !tpls["agent://{id}"] {
		t.Error("expected agent://{id} template")
	}
}

func TestPromptsGet_ReviewWorkflow(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name": "review-workflow",
		"arguments": map[string]string{
			"issueId": "42",
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "prompts/get",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	data, _ := json.Marshal(result["messages"])
	var msgs []map[string]interface{}
	json.Unmarshal(data, &msgs)
	if len(msgs) == 0 {
		t.Fatal("expected messages array")
	}
	content, _ := msgs[0]["content"].(map[string]interface{})
	text, _ := content["text"].(string)
	if text == "" {
		t.Error("expected non-empty prompt text")
	}
}

func TestPromptsGet_IssueTriage(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name": "issue-triage",
		"arguments": map[string]string{
			"issueId": "7",
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "prompts/get",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	data, _ := json.Marshal(result["messages"])
	var msgs []map[string]interface{}
	json.Unmarshal(data, &msgs)
	if len(msgs) == 0 {
		t.Fatal("expected messages array")
	}
	content, _ := msgs[0]["content"].(map[string]interface{})
	text, _ := content["text"].(string)
	if text == "" {
		t.Error("expected non-empty prompt text for issue-triage")
	}
	if !strings.Contains(text, "7") {
		t.Error("expected prompt to contain issue ID")
	}
}

func TestPromptsGet_Unknown(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"name":      "nonexistent",
		"arguments": map[string]string{},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "prompts/get",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for unknown prompt")
	}
}

func TestNotificationsInitialized(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "notifications/initialized",
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("unexpected error for notifications/initialized: %s", resp.Error.Message)
	}
}

func TestResourcesRead_Project(t *testing.T) {
	srv, projectSvc, _, _ := setupTest(t)

	proj, _ := projectSvc.Create("Test Project", "A test")
	projectSvc.AddMember(proj.ID, 1, models.ProjectRoleOwner)

	params, _ := json.Marshal(map[string]interface{}{
		"uri": "project://1",
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/read",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	contents, ok := result["contents"].([]interface{})
	if !ok || len(contents) == 0 {
		t.Fatal("expected contents array")
	}
	item := contents[0].(map[string]interface{})
	if item["uri"] != "project://1" {
		t.Errorf("expected uri project://1, got %v", item["uri"])
	}
	if item["mimeType"] != "application/json" {
		t.Errorf("expected mimeType application/json, got %v", item["mimeType"])
	}
	text, ok := item["text"].(string)
	if !ok || text == "" {
		t.Fatal("expected non-empty text")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("expected valid JSON in text: %v", err)
	}
	if parsed["name"] != "Test Project" {
		t.Errorf("expected name 'Test Project', got %v", parsed["name"])
	}
}

func TestResourcesRead_ProjectNotFound(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"uri": "project://999",
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/read",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent project")
	}
}

func TestResourcesRead_InvalidURI(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"uri": "unknown://foo",
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/read",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for unknown URI scheme")
	}
}

func TestResourcesRead_InvalidParams(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"bad": "data",
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/read",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for missing uri param")
	}
}

func TestResourcesRead_Issue(t *testing.T) {
	srv, projectSvc, agentSvc, _ := setupTest(t)

	proj, _ := projectSvc.Create("Project", "")
	agent, _ := agentSvc.Register("coder", "ai", "coder-rr", "pass", nil, "", "")
	projectSvc.AddMember(proj.ID, agent.ID, models.ProjectRoleMember)

	// Create an issue via MCP tool
	params, _ := json.Marshal(map[string]interface{}{
		"name": "create_issue",
		"arguments": map[string]interface{}{
			"title":       "Resource Test Issue",
			"description": "Testing resources/read for issue",
		},
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  params,
	}
	resp := srv.HandleRequest(req, agent.ID, "")
	if resp.Error != nil {
		t.Fatalf("create issue: %s", resp.Error.Message)
	}

	// Now read the issue resource
	params2, _ := json.Marshal(map[string]interface{}{
		"uri": "issue://1/1",
	})
	req2 := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "resources/read",
		Params:  params2,
	}
	resp2 := srv.HandleRequest(req2, 0, "")
	if resp2.Error != nil {
		t.Fatalf("read issue resource: %s", resp2.Error.Message)
	}
}

func TestResourcesRead_Agent(t *testing.T) {
	srv, _, agentSvc, _ := setupTest(t)

	agent, _ := agentSvc.Register("read-agent", "ai", "read-agent-001", "pass", []string{"CODING"}, "dev", "gpt-4")

	params, _ := json.Marshal(map[string]interface{}{
		"uri": "agent://1",
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/read",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error != nil {
		t.Fatalf("read agent resource: %s", resp.Error.Message)
	}

	// Verify agent data
	result := resp.Result.(map[string]interface{})
	contents := result["contents"].([]interface{})
	item := contents[0].(map[string]interface{})
	text := item["text"].(string)
	var parsed map[string]interface{}
	json.Unmarshal([]byte(text), &parsed)
	if parsed["name"] != agent.Name {
		t.Errorf("expected name %q, got %q", agent.Name, parsed["name"])
	}
	if parsed["externalId"] != "read-agent-001" {
		t.Errorf("expected externalId read-agent-001, got %v", parsed["externalId"])
	}
}

func TestResourcesRead_InvalidIssueURI(t *testing.T) {
	srv, _, _, _ := setupTest(t)

	params, _ := json.Marshal(map[string]interface{}{
		"uri": "issue://bad",
	})
	req := &mcp.Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "resources/read",
		Params:  params,
	}
	resp := srv.HandleRequest(req, 0, "")
	if resp.Error == nil {
		t.Fatal("expected error for invalid issue URI")
	}
}
