package panda

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/xid"
)

// TestPanda_Integration_AgentTimeline sends a full agent timeline message flow
// to the real server. Config is read from app/example/.env or a path set via
// PANDA_TEST_ENV. The test is skipped if no config file is found.
func TestPanda_Integration_AgentTimeline(t *testing.T) {
	cfg := loadIntegrationConfig(t)
	if cfg == nil {
		t.Skip("no integration config found")
	}

	server := cfg["server"]
	account := cfg["account"]
	password := cfg["password"]
	convName := cfg["conv_name"]

	httpClient := &http.Client{Timeout: 30 * time.Second}

	// ── 1. Login ──────────────────────────────────────────────
	t.Log("[1] Login")
	loginBody, _ := json.Marshal(map[string]string{
		"account":  account,
		"password": password,
	})
	req, _ := http.NewRequest(http.MethodPost, server+"/api/v1/users/login", strings.NewReader(string(loginBody)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			UserID string `json:"user_id"`
			Token  string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	if envelope.Code != 0 {
		t.Fatalf("login rejected: %s", envelope.Msg)
	}
	token := envelope.Data.Token
	userID := envelope.Data.UserID
	t.Logf("✓ Logged in as %s (user_id=%s)", account, userID)

	// ── 2. Find conversation ──────────────────────────────────
	t.Log("[2] Find Dolphin conversation")
	req, _ = http.NewRequest(http.MethodGet, server+"/api/v1/conversations", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err = httpClient.Do(req)
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var convEnvelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				ConvID string `json:"conv_id"`
				Name   string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&convEnvelope); err != nil {
		t.Fatalf("parse conversations: %v", err)
	}
	if convEnvelope.Code != 0 {
		t.Fatalf("list conversations rejected: %s", convEnvelope.Msg)
	}

	var convID string
	for _, c := range convEnvelope.Data.Items {
		if c.Name == convName {
			convID = c.ConvID
			break
		}
	}
	if convID == "" {
		names := make([]string, len(convEnvelope.Data.Items))
		for i, c := range convEnvelope.Data.Items {
			names[i] = c.Name
		}
		t.Fatalf("conversation '%s' not found. Available: %v", convName, names)
	}
	t.Logf("✓ Found: %s", convID)

	// ── 3. Connect WebSocket ──────────────────────────────────
	t.Log("[3] Connect WebSocket")
	wsURL := strings.Replace(server, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = wsURL + "/ws?token=" + token

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Use a persistent reader goroutine plus an ack channel.
	ackCh := make(chan int64, 10)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var f struct {
				Type    int             `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(msg, &f); err != nil {
				continue
			}
			if f.Type == 2 { // MsgSendAck
				var ack struct {
					MsgID     int64 `json:"msg_id"`
					ClientSeq int64 `json:"client_seq"`
					Status    int   `json:"status"`
				}
				if err := json.Unmarshal(f.Payload, &ack); err != nil {
					var raw string
					if err2 := json.Unmarshal(f.Payload, &raw); err2 == nil {
						_ = json.Unmarshal([]byte(raw), &ack)
					}
				}
				ackCh <- ack.MsgID
			}
		}
	}()

	// Wait for connection to settle (welcome frames arrive).
	time.Sleep(500 * time.Millisecond)
	t.Log("✓ WebSocket connected, ready")

	// ── 4. Send agent timeline messages ─────────────────────
	t.Log("[4] Send agent timeline messages")

	prefix := "来自dolphin的单元测试"

	type testFrame struct {
		Type    int             `json:"type"`
		ID      string          `json:"id"`
		Payload json.RawMessage `json:"payload"`
	}

	clientSeq := int64(0)
	nextSeq := func() int64 {
		clientSeq++
		return clientSeq
	}

	sendFrame := func(body AgentTimelineBody) error {
		seq := nextSeq()
		bodyJSON, _ := json.Marshal(body)
		payload, _ := json.Marshal(map[string]any{
			"conv_id":      convID,
			"content_type": 9,
			"body":         string(bodyJSON),
			"client_seq":   seq,
			"reply_to":     0,
			"mention":      []string{},
		})
		frame := testFrame{
			Type:    1, // MsgSend
			ID:      fmt.Sprintf("msg_%d", seq),
			Payload: payload,
		}
		data, _ := json.Marshal(frame)
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	recvAck := func(timeout time.Duration) int64 {
		select {
		case msgID := <-ackCh:
			return msgID
		case <-time.After(timeout):
			return 0
		}
	}

	entry := func(typ AgentTimelineEntryType, content, toolName, toolInput, status string) AgentTimelineEntry {
		return AgentTimelineEntry{
			ID:        xid.New().String(),
			Type:      typ,
			Content:   content,
			ToolName:  toolName,
			ToolInput: toolInput,
			Status:    status,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	// Step 1: Create bubble with thinking entry
	t.Log("  Step 1: Create bubble (thinking)")
	body1 := AgentTimelineBody{
		Title:   prefix + " - Researching...",
		Entries: []AgentTimelineEntry{entry(TimelineEntryThinking, "Let me analyze this question and prepare a response.", "", "", "")},
		Status:  "running",
	}
	if err := sendFrame(body1); err != nil {
		t.Fatalf("step 1 send: %v", err)
	}
	msgID1 := recvAck(10 * time.Second)
	if msgID1 == 0 {
		t.Fatal("Failed to get ack for step 1")
	}
	t.Logf("  ✓ Created bubble, msgID=%d", msgID1)
	time.Sleep(500 * time.Millisecond)

	// Step 2: Append toolCall entry
	t.Log("  Step 2: Append toolCall")
	body2 := AgentTimelineBody{
		Entries: []AgentTimelineEntry{entry(
			TimelineEntryToolCall,
			"Calling search_web to find latest information",
			"search_web",
			`{"query":"Go testing best practices","max_results":3}`,
			"running",
		)},
		Status:      "running",
		ParentMsgID: msgID1,
	}
	if err := sendFrame(body2); err != nil {
		t.Fatalf("step 2 send: %v", err)
	}
	msgID2 := recvAck(10 * time.Second)
	if msgID2 == 0 {
		t.Fatal("Failed to get ack for step 2")
	}
	t.Logf("  ✓ Appended toolCall, msgID=%d", msgID2)
	time.Sleep(500 * time.Millisecond)

	// Step 3: Append toolResult entry
	t.Log("  Step 3: Append toolResult")
	body3 := AgentTimelineBody{
		Entries: []AgentTimelineEntry{entry(
			TimelineEntryToolResult,
			"Found 3 results. Top result: Go 1.24 introduces improved testing features with better fuzzing support.",
			"search_web",
			"",
			"success",
		)},
		Status:      "running",
		ParentMsgID: msgID1,
	}
	if err := sendFrame(body3); err != nil {
		t.Fatalf("step 3 send: %v", err)
	}
	msgID3 := recvAck(10 * time.Second)
	if msgID3 == 0 {
		t.Fatal("Failed to get ack for step 3")
	}
	t.Logf("  ✓ Appended toolResult, msgID=%d", msgID3)
	time.Sleep(500 * time.Millisecond)

	// Step 4: Append response + complete
	t.Log("  Step 4: Append response + complete")
	body4 := AgentTimelineBody{
		Entries: []AgentTimelineEntry{entry(
			TimelineEntryResponse,
			prefix+" - 测试成功！\n\n"+
				"这是一个来自 dolphin bot 的 agent timeline 集成测试消息。\n\n"+
				"Timeline 包含以下条目:\n"+
				"1. thinking - 思考过程\n"+
				"2. toolCall - 工具调用 (search_web)\n"+
				"3. toolResult - 工具返回结果\n"+
				"4. response - 最终响应\n\n"+
				"如果你能看到这条消息，说明 agent timeline 协议工作正常。✅",
			"", "", "",
		)},
		Status:      "completed",
		ParentMsgID: msgID1,
	}
	if err := sendFrame(body4); err != nil {
		t.Fatalf("step 4 send: %v", err)
	}
	msgID4 := recvAck(10 * time.Second)
	if msgID4 == 0 {
		t.Fatal("Failed to get ack for step 4")
	}
	t.Logf("  ✓ Appended response, msgID=%d", msgID4)

	// ── 5. Summary ───────────────────────────────────────────
	t.Logf("Root msgID: %d (use for follow-up appends)", msgID1)
	t.Log("ALL STEPS PASSED")
}

// loadIntegrationConfig reads panda server connection details from a .env file.
// Searches PANDA_TEST_ENV, then app/example/.env, then .env (relative to working dir).
// Returns nil if no config file found. Env vars override file values.
func loadIntegrationConfig(t *testing.T) map[string]string {
	t.Helper()

	// Determine which .env file to read.
	envPath := os.Getenv("PANDA_TEST_ENV")
	if envPath == "" {
		// Walk up from working directory to find the .env file.
		wd, _ := os.Getwd()
		for dir := wd; dir != "" && dir != "/"; dir = filepath.Dir(dir) {
			for _, name := range []string{"app/example/.env", ".env"} {
				p := filepath.Join(dir, name)
				if _, err := os.Stat(p); err == nil {
					envPath = p
					break
				}
			}
			if envPath != "" {
				break
			}
		}
	}
	if envPath == "" {
		return nil
	}

	cfg := parseDotEnv(envPath)
	if cfg == nil {
		t.Logf("failed to parse config file: %s", envPath)
		return nil
	}

	// Env vars override file values.
	for _, k := range []string{"PANDA_SERVER", "PANDA_ACCOUNT", "PANDA_PASSWORD", "PANDA_CONV_NAME"} {
		if v := os.Getenv(k); v != "" {
			switch k {
			case "PANDA_SERVER":
				cfg["server"] = v
			case "PANDA_ACCOUNT":
				cfg["account"] = v
			case "PANDA_PASSWORD":
				cfg["password"] = v
			case "PANDA_CONV_NAME":
				cfg["conv_name"] = v
			}
		}
	}

	return cfg
}

// parseDotEnv parses a minimal .env file (no quoting, no interpolation).
func parseDotEnv(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	cfg := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		// Map .env keys to config keys.
		switch strings.ToUpper(k) {
		case "SERVER":
			cfg["server"] = v
		case "ACCOUNT":
			cfg["account"] = v
		case "PASSWORD":
			cfg["password"] = v
		case "CONV_NAME":
			cfg["conv_name"] = v
		}
	}
	if cfg["server"] == "" || cfg["account"] == "" || cfg["password"] == "" || cfg["conv_name"] == "" {
		return nil
	}
	return cfg
}
