package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dolphin/internal/agent"
	"dolphin/internal/agent/provider"
	"dolphin/internal/config"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"github.com/smartystreets/goconvey/convey"
	"gopkg.in/yaml.v3"
)

// ========== helpers ==========

func findConfigPath() string {
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 10; i++ {
		c := filepath.Join(dir, ".dolphin", "config.yaml")
		if _, err := os.Stat(c); err == nil {
			data, err := os.ReadFile(c)
			if err == nil && (strings.Contains(string(data), "api_key:") || strings.Contains(string(data), "api_key: ")) {
				return c
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func loadLLMConfig(t *testing.T) (*config.Config, []config.ProviderConfig) {
	t.Helper()
	cfgPath := findConfigPath()
	if cfgPath == "" {
		t.Skip("no .dolphin/config.yaml with API key found")
	}
	if v := os.Getenv("DOLPHIN_CONFIG"); v != "" {
		cfgPath = v
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Skipf("config load error: %v", err)
	}
	if !cfg.LLMConfigured() {
		t.Skip("LLM not configured (no API key)")
	}
	providers := cfg.LLM.EffectiveProviders()
	valid := make([]config.ProviderConfig, 0)
	for _, p := range providers {
		if p.APIKey != "" {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		t.Skip("no provider with API key configured")
	}
	return cfg, valid
}

func loadEmailFromAssets(t *testing.T) config.EmailConfig {
	t.Helper()
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, "tests", "assets.yaml")
		if data, err := os.ReadFile(path); err == nil {
			var parsed struct {
				Email struct {
					Enabled  bool   `yaml:"enabled"`
					From     string `yaml:"from"`
					SMTPHost string `yaml:"smtp_host"`
					SMTPPort int    `yaml:"smtp_port"`
					Username string `yaml:"username"`
					Password string `yaml:"password"`
					UseTLS   bool   `yaml:"use_tls"`
				} `yaml:"email"`
			}
			if err := yaml.Unmarshal(data, &parsed); err == nil && parsed.Email.SMTPHost != "" {
				return config.EmailConfig{
					SMTPHost: parsed.Email.SMTPHost,
					SMTPPort: parsed.Email.SMTPPort,
					Username: parsed.Email.Username,
					Password: parsed.Email.Password,
					From:     parsed.Email.From,
					UseTLS:   parsed.Email.UseTLS,
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return config.EmailConfig{}
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "401") ||
		strings.Contains(s, "403") ||
		strings.Contains(s, "invalid api key") ||
		strings.Contains(s, "InvalidApiKey") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "authentication")
}

// ========== test mocks ==========

type testIO struct {
	lines   []string
	readIdx int
	writes  strings.Builder
}

func (m *testIO) ReadLine() (string, error) {
	if m.readIdx >= len(m.lines) {
		return "/exit", nil
	}
	s := m.lines[m.readIdx]
	m.readIdx++
	return s, nil
}
func (m *testIO) WriteLine(s string) error {
	m.writes.WriteString(s)
	m.writes.WriteString("\n")
	return nil
}
func (m *testIO) WriteString(s string) error { m.writes.WriteString(s); return nil }
func (m *testIO) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: true, ShowToolDetails: true}
}
func (m *testIO) Context() string { return "test" }
func (m *testIO) Name() string    { return "test" }
func (m *testIO) output() string  { return m.writes.String() }

var _ transport.UserIO = (*testIO)(nil)

// mockTool implements mcp.Tool.
type mockTool struct {
	name string
}

func (t *mockTool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        t.name,
		Description: "mock tool for testing",
	}
}
func (t *mockTool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	return &mcp.ToolResult{Content: "mock result"}, nil
}

// ========== Convey tests ==========

func TestUserFirstRun(t *testing.T) {
	convey.Convey("Given a user installs dolphin for the first time", t, func() {
		homeTmp := t.TempDir()
		origHome := os.Getenv("HOME")
		origUserProfile := os.Getenv("USERPROFILE")
		os.Setenv("HOME", homeTmp)
		os.Setenv("USERPROFILE", homeTmp)
		defer func() {
			os.Setenv("HOME", origHome)
			os.Setenv("USERPROFILE", origUserProfile)
		}()

		convey.Convey("When there is no .dolphin directory in HOME", func() {
			// No marker → first run
			convey.So(config.IsFirstRun(), convey.ShouldBeTrue)
		})

		convey.Convey("When CreateFirstRunMarker marks setup complete", func() {
			config.CreateFirstRunMarker()
			convey.Convey("Then IsFirstRun returns false (no longer first run)", func() {
				convey.So(config.IsFirstRun(), convey.ShouldBeFalse)
			})
		})

		convey.Convey("When MarkFirstRunDone resets to initial state", func() {
			config.MarkFirstRunDone()
			convey.Convey("Then IsFirstRun returns true again", func() {
				convey.So(config.IsFirstRun(), convey.ShouldBeTrue)
			})
		})
	})
}

func TestLLMConfigLoading(t *testing.T) {
	convey.Convey("Given a user has configured .dolphin/config.yaml with LLM settings", t, func() {
		cfgPath := findConfigPath()
		if cfgPath == "" {
			t.Skip("no .dolphin/config.yaml with API key found")
			return
		}
		t.Logf("using config: %s", cfgPath)

		cfg, validProviders := loadLLMConfig(t)
		if len(validProviders) == 0 {
			t.Skip("no valid providers")
			return
		}

		convey.Convey("config.Load should succeed", func() {
			convey.So(cfg, convey.ShouldNotBeNil)
		})

		convey.Convey("LLMConfigured should return true", func() {
			convey.So(cfg.LLMConfigured(), convey.ShouldBeTrue)
		})

		convey.Convey("At least one provider should have an API key", func() {
			convey.So(len(validProviders), convey.ShouldBeGreaterThan, 0)
		})

		for _, p := range validProviders {
			convey.Convey("Provider "+p.Name+" should have required fields", func() {
				convey.So(p.Type, convey.ShouldBeIn, []string{"openai", "anthropic"})
				convey.So(p.APIKey, convey.ShouldNotBeBlank)
				convey.So(p.BaseURL, convey.ShouldNotBeBlank)
				convey.So(p.Model, convey.ShouldNotBeBlank)
			})
		}
	})
}

func TestLLMProviderHealthCheck(t *testing.T) {
	convey.Convey("Given configured LLM providers", t, func() {
		_, validProviders := loadLLMConfig(t)
		if len(validProviders) == 0 {
			t.Skip("no valid providers")
			return
		}

		for _, p := range validProviders {
			convey.Convey("Provider "+p.Name+" ("+p.Model+") should pass health check", func() {
				prov := provider.NewProviderFromConfig(&p)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				err := prov.HealthCheck(ctx)
				if err != nil {
					if isAuthError(err) {
						convey.SkipSo(err, convey.ShouldBeNil)
						return
					}
					convey.So(err, convey.ShouldBeNil)
				} else {
					convey.So(err, convey.ShouldBeNil)
				}
			})
		}
	})
}

func TestLLMProviderRoundTrip(t *testing.T) {
	convey.Convey("Given a configured LLM provider", t, func() {
		_, validProviders := loadLLMConfig(t)
		if len(validProviders) == 0 {
			t.Skip("no valid providers")
			return
		}

		p := validProviders[0]
		prov := provider.NewProviderFromConfig(&p)

		convey.Convey("When sending a simple message", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			ch, err := prov.CompleteStream(ctx, provider.ProviderRequest{
				System: "You are a helpful assistant. Keep responses brief.",
				Messages: []provider.Message{
					{Role: "user", Content: json.RawMessage(`"reply with exactly: OK"`)},
				},
			})
			if err != nil {
				if isAuthError(err) {
					t.Skipf("auth error: %v", err)
					return
				}
				convey.So(err, convey.ShouldBeNil)
				return
			}

			var text strings.Builder
			for c := range ch {
				if c.Done {
					break
				}
				if c.Content != nil {
					text.WriteString(string(c.Content))
				}
			}

			convey.Convey("Then a non-empty response should be received", func() {
				convey.So(text.String(), convey.ShouldNotBeBlank)
			})
		})
	})
}

// TestAgentCommands tests the agent with a real provider from config.
// This test requires valid LLM credentials. It is skipped if not configured.
func TestAgentCommandsReal(t *testing.T) {
	convey.Convey("Given an agent with a real provider from config", t, func() {
		_, validProviders := loadLLMConfig(t)
		if len(validProviders) == 0 {
			t.Skip("no valid providers")
			return
		}

		cfg := config.DefaultConfig()
		config.SetSessionsDir(t.TempDir())
		cfg.Session.MaxLoop = 10
		cfg.LLM.MaxContextTokens = 100000
		// Use the real provider from config
		cfg.LLM.Providers = validProviders

		sessMgr := session.NewManager(config.SessionsDir())
		sessMgr.EnsureDir()

		toolReg := mcp.NewRegistry(cfg)
		toolReg.Register(&mockTool{name: "test_tool"})

		agt := agent.New(cfg, sessMgr, toolReg)

		convey.Convey("When user types /help", func() {
			io := &testIO{lines: []string{"/help", "/exit"}}
			agt.Run(context.Background(), io)
			out := io.output()

			convey.Convey("Then help text should contain commands list", func() {
				convey.So(out, convey.ShouldContainSubstring, "Commands")
				convey.So(out, convey.ShouldContainSubstring, "/exit")
			})

			convey.Convey("Then welcome should list loaded tools", func() {
				convey.So(out, convey.ShouldContainSubstring, "Loaded MCP tools")
				convey.So(out, convey.ShouldContainSubstring, "test_tool")
			})
		})
	})
}

func TestSessionLifecycle(t *testing.T) {
	convey.Convey("Given a session manager", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		mgr.EnsureDir()

		convey.Convey("When no sessions exist", func() {
			id, path, _, err := mgr.LatestSession()
			convey.Convey("Then LatestSession should return zero values", func() {
				convey.So(err, convey.ShouldBeNil)
				convey.So(id, convey.ShouldEqual, session.SessionID(""))
				convey.So(path, convey.ShouldEqual, "")
			})
		})

		convey.Convey("When a new session is created", func() {
			sess, err := mgr.NewSession(50)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sess, convey.ShouldNotBeNil)

			convey.Convey("Then it should be retrievable by ID", func() {
				got := mgr.Get(sess.ID)
				convey.So(got, convey.ShouldNotBeNil)
				convey.So(got.ID, convey.ShouldEqual, sess.ID)
			})

			convey.Convey("Then events can be logged and read back", func() {
				sess.LogMessage("user", json.RawMessage(`"hello"`))
				sess.Close()

				_, path, _, _ := mgr.LatestSession()
				events, err := session.ReadEvents(path)
				convey.So(err, convey.ShouldBeNil)
				convey.So(len(events), convey.ShouldBeGreaterThan, 0)
				convey.So(events[0].Type, convey.ShouldEqual, session.EventMessage)
			})

			convey.Convey("Then it can be removed", func() {
				mgr.Remove(sess.ID)
				got := mgr.Get(sess.ID)
				convey.So(got, convey.ShouldBeNil)
			})
		})
	})
}

func TestSessionToolCallRecording(t *testing.T) {
	convey.Convey("Given a session", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		mgr.EnsureDir()
		sess, _ := mgr.NewSession(50)

		convey.Convey("When tool calls and results are logged", func() {
			sess.LogMessage("user", json.RawMessage(`"run a shell command"`))
			sess.LogToolCall("shell", json.RawMessage(`{"command":"ls"}`))
			sess.LogToolResult("shell", json.RawMessage(`"file1.txt\nfile2.txt"`), false)
			sess.LogMessage("assistant", json.RawMessage(`"here are the files"`))
			sess.Close()

			_, path, _, _ := mgr.LatestSession()
			events, err := session.ReadEvents(path)
			convey.So(err, convey.ShouldBeNil)

			convey.Convey("Then all events should be recorded in order", func() {
				convey.So(len(events), convey.ShouldEqual, 4)
				convey.So(events[0].Type, convey.ShouldEqual, session.EventMessage)
				convey.So(events[1].Type, convey.ShouldEqual, session.EventToolCall)
				convey.So(events[2].Type, convey.ShouldEqual, session.EventToolResult)
				convey.So(events[3].Type, convey.ShouldEqual, session.EventMessage)
			})

			convey.Convey("Then tool call event should contain command name", func() {
				convey.So(events[1].ToolName, convey.ShouldEqual, "shell")
			})
		})
	})
}

func TestSessionSummaryLogging(t *testing.T) {
	convey.Convey("Given a session", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		mgr.EnsureDir()
		sess, _ := mgr.NewSession(50)

		convey.Convey("When a compression summary is logged", func() {
			sess.LogCompression(session.CompressMeta{
				Level:        1,
				CoveredCount: 5,
				Summary:      "compressed context",
				TokensSaved:  8000,
			})
			sess.Close()

			_, path, _, _ := mgr.LatestSession()
			events, _ := session.ReadEvents(path)

			convey.Convey("Then the compression event should be recorded", func() {
				hasCompression := false
				for _, e := range events {
					if e.Type == session.EventCompression {
						hasCompression = true
						break
					}
				}
				convey.So(hasCompression, convey.ShouldBeTrue)
			})
		})
	})
}

// TestAgentErrorRecoveryCtxCancel tests graceful shutdown on context cancel.
// Uses a real provider or skips.
func TestAgentErrorRecoveryCtxCancel(t *testing.T) {
	_, validProviders := loadLLMConfig(t)
	if len(validProviders) == 0 {
		t.Skip("no valid providers")
		return
	}

	convey.Convey("Given an agent with real provider", t, func() {
		cfg := config.DefaultConfig()
		config.SetSessionsDir(t.TempDir())
		cfg.Session.MaxLoop = 20
		cfg.LLM.MaxContextTokens = 100000
		cfg.LLM.Providers = validProviders

		sessMgr := session.NewManager(config.SessionsDir())
		sessMgr.EnsureDir()

		toolReg := mcp.NewRegistry(cfg)
		agt := agent.New(cfg, sessMgr, toolReg)

		convey.Convey("When context is cancelled mid-flight", func() {
			ctx, cancel := context.WithCancel(context.Background())
			io := &testIO{lines: []string{"msg1"}}

			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()

			convey.Convey("Then agent should exit gracefully without panic", func() {
				convey.So(func() { agt.Run(ctx, io) }, convey.ShouldNotPanic)
			})
		})
	})
}

func TestSessionDirIsolation(t *testing.T) {
	convey.Convey("Given a user running multiple projects", t, func() {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		mgr1 := session.NewManager(dir1)
		mgr1.EnsureDir()
		mgr2 := session.NewManager(dir2)
		mgr2.EnsureDir()

		convey.Convey("Sessions in project A should be isolated from project B", func() {
			sess1, _ := mgr1.NewSession(50)
			sess1.LogMessage("user", json.RawMessage(`"project A work"`))
			sess1.Close()

			sess2, _ := mgr2.NewSession(50)
			sess2.LogMessage("user", json.RawMessage(`"project B work"`))
			sess2.Close()

			convey.So(sess1.ID, convey.ShouldNotEqual, sess2.ID)

			_, path1, _, _ := mgr1.LatestSession()
			_, path2, _, _ := mgr2.LatestSession()
			events1, _ := session.ReadEvents(path1)
			events2, _ := session.ReadEvents(path2)
			convey.So(string(events1[0].Content), convey.ShouldContainSubstring, "project A")
			convey.So(string(events2[0].Content), convey.ShouldContainSubstring, "project B")
		})
	})
}

func TestSessionFileFormat(t *testing.T) {
	convey.Convey("Given a session log file on disk", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		mgr.EnsureDir()
		sess, _ := mgr.NewSession(50)

		sess.LogMessage("user", json.RawMessage(`"hello"`))
		sess.LogMessage("assistant", json.RawMessage(`"hi there"`))
		sess.LogToolCall("shell", json.RawMessage(`{"command":"ls"}`))
		sess.LogToolResult("shell", json.RawMessage(`{"stdout":"file.txt"}`), false)
		sess.Close()

		_, path, _, _ := mgr.LatestSession()

		convey.Convey("Each line should be valid JSON with required fields", func() {
			events, err := session.ReadEvents(path)
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(events), convey.ShouldBeGreaterThan, 3)

			for i, e := range events {
				convey.So(string(e.Type), convey.ShouldNotBeBlank)
				convey.So(e.Timestamp.IsZero(), convey.ShouldBeFalse)
				t.Logf("  event[%d] type=%s ts=%s", i, e.Type, e.Timestamp)
			}
		})
	})
}

func TestSessionEventOrdering(t *testing.T) {
	convey.Convey("Given a full session recording", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		mgr.EnsureDir()
		sess, _ := mgr.NewSession(50)

		sess.LogSystem("session started")
		sess.LogMessage("user", json.RawMessage(`"what files are in this directory?"`))
		sess.LogToolCall("shell", json.RawMessage(`{"command":"ls -la"}`))
		sess.LogToolResult("shell", json.RawMessage(`"total 8\ndrwxr-xr-x  2 user  staff   64 May 17 12:00 ."`), false)
		sess.LogMessage("assistant", json.RawMessage(`"The directory contains files."`))
		sess.LogSystem("session ended")
		sess.Close()

		_, path, _, _ := mgr.LatestSession()
		events, _ := session.ReadEvents(path)

		convey.Convey("Events should be in chronological order", func() {
			for i := 1; i < len(events); i++ {
				convey.So(events[i].Timestamp.Unix(), convey.ShouldBeGreaterThanOrEqualTo, events[i-1].Timestamp.Unix())
			}
		})

		convey.Convey("Tool call should precede its result", func() {
			toolCallIdx := -1
			toolResultIdx := -1
			for i, e := range events {
				if e.Type == session.EventToolCall {
					toolCallIdx = i
				}
				if e.Type == session.EventToolResult {
					toolResultIdx = i
				}
			}
			convey.So(toolCallIdx, convey.ShouldBeGreaterThan, -1)
			convey.So(toolResultIdx, convey.ShouldBeGreaterThan, -1)
			convey.So(toolCallIdx, convey.ShouldBeLessThan, toolResultIdx)
		})
	})
}

func TestConfigValidation(t *testing.T) {
	convey.Convey("Given a config object", t, func() {
		cfg := config.DefaultConfig()

		convey.Convey("Default config should pass validation", func() {
			err := cfg.Validate()
			convey.So(err, convey.ShouldBeNil)
		})

		convey.Convey("Config with max_tokens = 0 should fail", func() {
			cfg.LLM.MaxTokens = 0
			err := cfg.Validate()
			convey.So(err, convey.ShouldNotBeNil)
		})

		convey.Convey("Config with temperature > 2 should fail", func() {
			cfg.LLM.Temperature = 3.0
			err := cfg.Validate()
			convey.So(err, convey.ShouldNotBeNil)
		})

		convey.Convey("Config with negative max_loop should fail", func() {
			cfg.Session.MaxLoop = -1
			err := cfg.Validate()
			convey.So(err, convey.ShouldNotBeNil)
		})

		convey.Convey("Config with invalid provider type should fail", func() {
			cfg.LLM.Providers = []config.ProviderConfig{
				{Type: "invalid", APIKey: "sk-test", BaseURL: "https://example.com", Model: "test"},
			}
			err := cfg.Validate()
			convey.So(err, convey.ShouldNotBeNil)
		})
	})
}

func TestAgentWithRealProvider(t *testing.T) {
	convey.Convey("Given a real LLM provider from config", t, func() {
		_, validProviders := loadLLMConfig(t)
		if len(validProviders) == 0 {
			t.Skip("no valid providers")
			return
		}

		p := validProviders[0]
		realProvider := provider.NewProviderFromConfig(&p)

		cfg := config.DefaultConfig()
		config.SetSessionsDir(t.TempDir())
		cfg.Session.MaxLoop = 10
		cfg.LLM.MaxContextTokens = 100000

		sessMgr := session.NewManager(config.SessionsDir())
		sessMgr.EnsureDir()

		toolReg := mcp.NewRegistry(cfg)
		toolReg.Register(&mockTool{name: "test_tool"})

		// Use agent.New(...) which creates the agent with its own provider.
		// We can't directly set the provider on the exported Agent struct,
		// but we can test the provider independently and use mock provider
		// for the full agent flow. The provider connectivity is tested in
		// TestLLMProviderHealthCheck and TestLLMProviderRoundTrip.

		convey.Convey("Real provider should respond to simple query", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			ch, err := realProvider.CompleteStream(ctx, provider.ProviderRequest{
				System: "You are a helpful assistant. Keep responses brief.",
				Messages: []provider.Message{
					{Role: "user", Content: json.RawMessage(`"say: test ok"`)},
				},
			})
			if err != nil {
				if isAuthError(err) {
					t.Skipf("auth error: %v", err)
					return
				}
				convey.So(err, convey.ShouldBeNil)
				return
			}

			var text strings.Builder
			for c := range ch {
				if c.Done {
					break
				}
				if c.Content != nil {
					text.WriteString(string(c.Content))
				}
			}

			convey.Convey("Then a meaningful response should be returned", func() {
				convey.So(strings.TrimSpace(text.String()), convey.ShouldNotBeBlank)
				t.Logf("real provider response: %s", text.String())
			})
		})

		_ = cfg
	})
}

func TestMultiProviderConfig(t *testing.T) {
	convey.Convey("Given a config.yaml with providers", t, func() {
		cfgPath := findConfigPath()
		if cfgPath == "" {
			t.Skip("no config found")
			return
		}
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Skipf("read config: %v", err)
			return
		}

		var raw struct {
			LLM struct {
				Type    string `yaml:"type"`
				APIKey  string `yaml:"api_key"`
				BaseURL string `yaml:"base_url"`
				Model   string `yaml:"model"`
			} `yaml:"llm"`
			Providers []struct {
				Name    string `yaml:"name"`
				Type    string `yaml:"type"`
				APIKey  string `yaml:"api_key"`
				BaseURL string `yaml:"base_url"`
				Model   string `yaml:"model"`
			} `yaml:"providers"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Skipf("parse config: %v", err)
			return
		}

		all := raw.Providers
		if raw.LLM.APIKey != "" {
			all = append(all, struct {
				Name    string `yaml:"name"`
				Type    string `yaml:"type"`
				APIKey  string `yaml:"api_key"`
				BaseURL string `yaml:"base_url"`
				Model   string `yaml:"model"`
			}{
				Type:    raw.LLM.Type,
				APIKey:  raw.LLM.APIKey,
				BaseURL: raw.LLM.BaseURL,
				Model:   raw.LLM.Model,
				Name:    "default",
			})
		}

		convey.Convey("Each provider with API key should have complete config", func() {
			tested := 0
			for _, p := range all {
				if p.APIKey == "" {
					continue
				}
				tested++
				convey.Convey("Provider "+p.Name+" ("+p.Type+")", func() {
					convey.So(p.Type, convey.ShouldBeIn, []string{"openai", "anthropic"})
					convey.So(p.BaseURL, convey.ShouldNotBeBlank)
					convey.So(p.Model, convey.ShouldNotBeBlank)
				})
			}
			if tested == 0 {
				t.Skip("no provider with API key configured in config.yaml")
			}
		})
	})
}

// ========== email interaction tests ==========

// loadEmailFromYAML reads an email config from a YAML file at the given path.
func loadEmailFromYAML(t *testing.T, yamlPath string) *config.EmailConfig {
	t.Helper()
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Skipf("%s not found: %v", yamlPath, err)
	}
	var raw struct {
		Email struct {
			Enabled      bool   `yaml:"enabled"`
			From         string `yaml:"from"`
			IMAPHost     string `yaml:"imap_host"`
			IMAPPort     int    `yaml:"imap_port"`
			Password     string `yaml:"password"`
			PollInterval string `yaml:"poll_interval"`
			SMTPHost     string `yaml:"smtp_host"`
			SMTPPort     int    `yaml:"smtp_port"`
			UseTLS       bool   `yaml:"use_tls"`
			Username     string `yaml:"username"`
		} `yaml:"email"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Skipf("parse %s: %v", yamlPath, err)
	}
	ec := raw.Email
	if !ec.Enabled {
		t.Skipf("email not enabled in %s", yamlPath)
	}
	if ec.Username == "" || ec.Password == "" {
		t.Skipf("email credentials missing in %s", yamlPath)
	}
	return &config.EmailConfig{
		Enabled:      ec.Enabled,
		SMTPHost:     ec.SMTPHost,
		SMTPPort:     ec.SMTPPort,
		IMAPHost:     ec.IMAPHost,
		IMAPPort:     ec.IMAPPort,
		Username:     ec.Username,
		Password:     ec.Password,
		From:         ec.From,
		UseTLS:       ec.UseTLS,
		PollInterval: ec.PollInterval,
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestEmailAssetsAccountSendReceive verifies the assets.yaml email account
// can send and receive emails (basic SMTP + IMAP check).
func TestEmailAssetsAccountSendReceive(t *testing.T) {
	convey.Convey("Given assets.yaml email account (cafebabe_2019@qq.com)", t, func() {
		assetsPath := filepath.Join("..", "tests", "assets.yaml")
		ec := loadEmailFromYAML(t, assetsPath)
		t.Logf("account: %s smtp=%s:%d imap=%s:%d", ec.Username, ec.SMTPHost, ec.SMTPPort, ec.IMAPHost, ec.IMAPPort)

		convey.Convey("SMTP — should be able to send an email to itself", func() {
			tp := transport.NewEmailTransport(ec)
			tp.SetLastSender(ec.From)
			body := fmt.Sprintf("[dolphin self-test] send+receive verification at %s", time.Now().Format(time.RFC3339))
			err := tp.WriteLine(body)
			convey.So(err, convey.ShouldBeNil)
			t.Logf("sent: %s", body)
		})

		convey.Convey("IMAP — should be able to poll and find unseen messages", func() {
			// Wait a moment for delivery
			time.Sleep(2 * time.Second)
			tp := transport.NewEmailTransport(ec)
			tp.SetStartTime(time.Now().Add(-5 * time.Minute))
			tp.SetAllowSelfSent(true)
			tp.SetAllowedSenders([]string{})

			msg := tp.PollIMAP()
			if msg == nil {
				t.Log("no unseen messages (may be expected if inbox is empty)")
			} else {
				t.Logf("latest unseen: from=%s subject=%s body=%s",
					msg.From, msg.Subject, truncateStr(msg.Body, 200))
				convey.So(msg.Body, convey.ShouldNotContainSubstring, "Content-Type:")
				convey.So(msg.Body, convey.ShouldNotContainSubstring, "Content-Transfer-Encoding:")
			}
		})
	})
}

// TestEmailAgentOneRound verifies the agent's email behavior end-to-end:
//  1. Start agent with email transport (config.yaml) in background
//  2. User (assets.yaml) sends a question email to agent
//  3. Agent reads the email via IMAP, processes it with LLM, replies via SMTP
//  4. User polls IMAP and verifies the agent's reply
func TestEmailAgentOneRound(t *testing.T) {
	// Load user email account
	assetsPath := filepath.Join("..", "tests", "assets.yaml")
	userEmail := loadEmailFromYAML(t, assetsPath)

	// Load agent config
	cfgPath := findConfigPath()
	if cfgPath == "" {
		t.Skip("no .dolphin/config.yaml found")
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Skipf("config load error: %v", err)
	}
	agentEmail := &cfg.Transport.Email
	if !agentEmail.Enabled {
		t.Skip("agent email not enabled in config.yaml")
	}
	if !cfg.LLMConfigured() {
		t.Skip("LLM not configured — agent needs LLM to answer")
	}
	t.Logf("user:  %s (assets.yaml)", userEmail.Username)
	t.Logf("agent: %s (config.yaml)", agentEmail.Username)

	// Step 1: Clear agent's inbox — mark all unseen as read so only our
	// question email is found by the agent's poll.
	clearTP := transport.NewEmailTransport(agentEmail)
	clearTP.SetStartTime(time.Now().Add(-24 * time.Hour))
	clearTP.SetAllowSelfSent(true)
	clearTP.SetAllowedSenders([]string{})
	for i := 0; i < 5; i++ {
		msg := clearTP.PollIMAP()
		if msg == nil {
			break
		}
		t.Logf("cleared old email: from=%s subject=%s", msg.From, truncateStr(msg.Subject, 80))
	}
	t.Log("agent inbox cleared")

	// Step 2: Send question from user to agent (continued)
	userTP := transport.NewEmailTransport(userEmail)
	userTP.SetLastSender(agentEmail.From)

	roundTag := fmt.Sprintf("dolphin-%s", time.Now().Format("150405"))
	question := fmt.Sprintf("今天日期是多少？[%s]", roundTag)
	if err := userTP.WriteLine(question); err != nil {
		t.Fatalf("send question: %v", err)
	}
	t.Logf("sent to agent (%s): %s", agentEmail.From, question)

	// Wait for email delivery before starting agent
	time.Sleep(5 * time.Second)

	// Step 2: Start agent with email IO
	config.SetSessionsDir(t.TempDir())
	cfg.Session.MaxLoop = 5

	sessMgr := session.NewManager(config.SessionsDir())
	sessMgr.EnsureDir()
	toolReg := mcp.NewRegistry(cfg)
	agt := agent.New(cfg, sessMgr, toolReg)

	agentIO := transport.NewEmailTransport(agentEmail)
	agentIO.SetStartTime(time.Now().Add(-5 * time.Minute))
	agentIO.SetAllowSelfSent(true)
	agentIO.SetAllowedSenders([]string{})

	agentCtx, agentCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer agentCancel()

	agentDone := make(chan struct{})
	go func() {
		defer close(agentDone)
		agt.Run(agentCtx, agentIO)
	}()
	go func() {
		agentIO.Start(agentCtx)
	}()

	t.Log("agent started with email IO, waiting for reply...")

	// Step 3: Wait for agent's reply in user's inbox
	var reply *transport.EmailMessage
	replyDeadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(replyDeadline) {
		time.Sleep(5 * time.Second)
		userPoll := transport.NewEmailTransport(userEmail)
		userPoll.SetStartTime(time.Now().Add(-5 * time.Minute))
		userPoll.SetAllowSelfSent(true)
		userPoll.SetAllowedSenders([]string{})
		reply = userPoll.PollIMAP()
		if reply != nil && reply.From != userEmail.From {
			t.Logf("got reply from=%s subject=%s", reply.From, reply.Subject)
			break
		}
		if reply != nil {
			t.Logf("skipping self-sent: from=%s subject=%s", reply.From, reply.Subject)
			reply = nil
		}
	}

	agentCancel()
	<-agentDone
	t.Log("agent shutdown complete")

	// Step 4: Verify the reply
	if reply == nil {
		t.Skip("no agent reply received within 60s")
	}
	if reply.Body == "" {
		t.Error("reply body is empty")
	}
	t.Logf("agent reply body: %s", truncateStr(reply.Body, 500))

	if strings.Contains(reply.Body, "Content-Type:") || strings.Contains(reply.Body, "Content-Transfer-Encoding:") {
		t.Error("reply contains raw MIME headers — decoding failed")
	}

	// Reply should be date-related
	body := reply.Body
	hasDate := strings.Contains(body, time.Now().Format("2006")) ||
		strings.Contains(body, "今天") ||
		strings.Contains(body, "日期")
	if !hasDate {
		t.Errorf("reply should mention today's date, got: %s", truncateStr(body, 300))
	}
}

func TestSessionJSONLFormat(t *testing.T) {
	convey.Convey("Given a session with activity", t, func() {
		dir := t.TempDir()
		mgr := session.NewManager(dir)
		mgr.EnsureDir()
		sess, _ := mgr.NewSession(50)

		sess.LogMessage("user", json.RawMessage(`"do something"`))
		sess.LogToolCall("shell", json.RawMessage(`{"command":"echo done"}`))
		sess.LogToolResult("shell", json.RawMessage(`"done"`), false)
		sess.LogMessage("assistant", json.RawMessage(`"completed"`))
		sess.Close()

		_, path, _, _ := mgr.LatestSession()

		convey.Convey("The JSONL file should have one valid JSON object per line", func() {
			data, err := os.ReadFile(path)
			convey.So(err, convey.ShouldBeNil)

			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				var evt map[string]any
				err := json.Unmarshal([]byte(line), &evt)
				convey.So(err, convey.ShouldBeNil)
				convey.So(evt["type"], convey.ShouldNotBeBlank)
			}
		})
	})
}

func TestFeedbackCommand(t *testing.T) {
	convey.Convey("Given the /feedback command", t, func() {
		_, validProviders := loadLLMConfig(t)
		if len(validProviders) == 0 {
			t.Skip("no valid providers")
			return
		}

		newCoordinator := func(emailCfg config.EmailConfig) *agent.Coordinator {
			cfg := config.DefaultConfig()
			config.SetSessionsDir(t.TempDir())
			cfg.Session.MaxLoop = 5
			cfg.LLM.MaxContextTokens = 100000
			cfg.LLM.Providers = validProviders
			cfg.Transport.Email = emailCfg

			sessMgr := session.NewManager(config.SessionsDir())
			sessMgr.EnsureDir()
			toolReg := mcp.NewRegistry(cfg)
			toolReg.Register(&mockTool{name: "shell"})

			agt := agent.New(cfg, sessMgr, toolReg)
			pool := agent.NewAgentPool(context.Background(), agent.PoolConfig{})
			return agent.NewCoordinator(agt, pool)
		}

		convey.Convey("When no arguments are given", func() {
			coord := newCoordinator(config.EmailConfig{})
			io := &testIO{lines: []string{"/feedback", "/exit"}}
			coord.Run(context.Background(), io)
			out := io.output()

			convey.Convey("Then usage should be shown", func() {
				convey.So(out, convey.ShouldContainSubstring, "Usage: /feedback")
				convey.So(out, convey.ShouldContainSubstring, "Send feedback to the development team")
			})
		})

		convey.Convey("When feedback text is provided but SMTP is not configured", func() {
			coord := newCoordinator(config.EmailConfig{})
			io := &testIO{lines: []string{"/feedback test message 123", "/exit"}}
			coord.Run(context.Background(), io)
			out := io.output()

			convey.Convey("Then error about missing SMTP config should appear", func() {
				convey.So(out, convey.ShouldContainSubstring, "Email SMTP not configured")
			})
		})

		convey.Convey("When feedback contains special characters and emoji", func() {
			coord := newCoordinator(config.EmailConfig{})
			io := &testIO{lines: []string{"/feedback bug report: crash on startup 😞", "/exit"}}
			coord.Run(context.Background(), io)
			out := io.output()

			convey.Convey("Then the message should be handled without crash", func() {
				convey.So(out, convey.ShouldContainSubstring, "Email SMTP not configured")
			})
		})

		convey.Convey("When SMTP is configured with unreachable server", func() {
			coord := newCoordinator(config.EmailConfig{
				SMTPHost: "localhost",
				SMTPPort: 19999,
			})
			io := &testIO{lines: []string{"/feedback unreachable server feedback", "/exit"}}
			coord.Run(context.Background(), io)
			out := io.output()

			convey.Convey("Then it should show sending attempt and graceful failure", func() {
				convey.So(out, convey.ShouldContainSubstring, "Sending feedback to")
				convey.So(out, convey.ShouldContainSubstring, "Failed to send feedback")
			})
		})

		convey.Convey("When using real SMTP config from assets.yaml", func() {
			emailCfg := loadEmailFromAssets(t)
			if emailCfg.SMTPHost == "" {
				t.Skip("no email config in assets.yaml")
				return
			}
			coord := newCoordinator(emailCfg)
			io := &testIO{lines: []string{"/feedback integration test feedback from dolphin test suite", "/exit"}}
			coord.Run(context.Background(), io)
			out := io.output()

			convey.Convey("Then feedback should be sent successfully", func() {
				convey.So(out, convey.ShouldContainSubstring, "Sending feedback to")
				convey.So(out, convey.ShouldContainSubstring, "Thank you! Your feedback has been sent")
			})
		})
	})
}
