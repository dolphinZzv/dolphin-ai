package agent

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/smtp"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"dolphin/internal/agent/console"
	"dolphin/internal/agent/provider"
	"dolphin/internal/config"
	"dolphin/internal/i18n"
	"dolphin/internal/registry"
	"dolphin/internal/session"
	"dolphin/internal/transport"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// cmdUserIO adapts a cobra.Command's OutOrStdout to the transport.UserIO
// interface, so existing handler methods can be reused from cobra RunE closures.
type cmdUserIO struct{ cmd *cobra.Command }

func (w *cmdUserIO) WriteLine(s string) error {
	fmt.Fprintln(w.cmd.OutOrStdout(), s)
	return nil
}

func (w *cmdUserIO) WriteString(s string) error {
	fmt.Fprint(w.cmd.OutOrStdout(), s)
	return nil
}

func (w *cmdUserIO) ReadLine() (string, error) {
	return "", io.EOF
}

func (w *cmdUserIO) Flush() error { return nil }

func (w *cmdUserIO) Capabilities() transport.Capabilities { return transport.Capabilities{} }

func (w *cmdUserIO) Context() string { return "" }

func (w *cmdUserIO) Name() string { return "cmd" }

// onboardConsole registers all built-in console commands in the unified
// registry. The old console is kept as an empty fallthrough for safety.
func (c *Coordinator) onboardConsole() {
	con := console.New()
	c.console = con

	reg := c.reg

	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "help",
			Hidden: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c.printHelp(&cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatSystem,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "mcp",
			Short:  "List MCP tools",
			Hidden: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c.printMCP(&cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatMCP,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "agents",
			Short:  "List agents and their status",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c.printAgents(&cmdUserIO{cmd: cmd}, args)
				return nil
			},
		},
		Category: registry.CatAgent,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "crontab",
			Short:  "List scheduled tasks",
			Hidden: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c.printCronTasks(&cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatCron,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "model",
			Short:  "Show or switch the active model",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c.handleModelCmd(args, &cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatConfig,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "cancel",
			Short:  "Cancel running tasks",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c.handleCancelCmd(args, &cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatAgent,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "forget",
			Short:  "Reset agent conversation context",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Usage: /forget <agentname>")
					fmt.Fprintln(cmd.OutOrStdout(), "Example: /forget sheller")
					return nil
				}
				if err := c.pool.Forget(args[0]); err != nil {
					fmt.Fprintln(cmd.OutOrStdout(), "Error:", err)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Agent %q conversation context reset.\n", args[0])
				}
				return nil
			},
		},
		Category: registry.CatAgent,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "context",
			Short:  "Show agent context summary",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c.printContext(args, &cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatAgent,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "feedback",
			Short:  "Send feedback to the development team",
			Hidden: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				c.handleFeedback(args, &cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatSystem,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "transport",
			Short:  "Show transport configuration",
			Hidden: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c.printTransport(&cmdUserIO{cmd: cmd})
				return nil
			},
		},
		Category: registry.CatSystem,
	})
	reg.Register(&registry.CommandSpec{
		Cobra: &cobra.Command{
			Use:    "reload",
			Short:  "Reload the agent",
			Hidden: true,
			RunE: func(cmd *cobra.Command, _ []string) error {
				c.reloadRequested = true
				fmt.Fprintln(cmd.OutOrStdout(), "Reloading agent...")
				return nil
			},
		},
		Category: registry.CatSystem,
		Signal:   registry.SignalReload,
	})

	// Config command with get/set subcommands.
	configCmd := &cobra.Command{
		Use:    "config",
		Short:  "Read and modify runtime configuration",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c.handleConfigListCmd(&cmdUserIO{cmd: cmd})
			return nil
		},
	}
	configCmd.AddCommand(&cobra.Command{
		Use:    "get",
		Short:  "Read a configuration value by path",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c.handleConfigGetCmd(args, &cmdUserIO{cmd: cmd})
			return nil
		},
	})
	configCmd.AddCommand(&cobra.Command{
		Use:    "set",
		Short:  "Modify a configuration value",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c.handleConfigSetCmd(args, &cmdUserIO{cmd: cmd})
			return nil
		},
	})
	reg.Register(&registry.CommandSpec{
		Cobra:    configCmd,
		Category: registry.CatConfig,
	})
}

func (c *Coordinator) printHelp(io transport.UserIO) {
	var sb strings.Builder
	sb.WriteString(i18n.TL(i18n.KeyHelpHeader))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpHelp))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpStatus))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpContext))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpConfig))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpExit))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpReload))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpCancel))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpCancelID))
	sb.WriteString("\n")
	sb.WriteString("\n")
	sb.WriteString("  /forget <name>: Reset conversation context for an agent\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpAgents))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpSkills))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpCommands))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpWorkflow))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpSessions))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpCron))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpMCP))
	sb.WriteString("\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpModel))
	sb.WriteString("\n\n")
	sb.WriteString(i18n.TL(i18n.KeyHelpTopMCP))
	sb.WriteString("\n")
	stats := c.agent.toolReg.ToolStats()
	toolDefs := c.agent.toolReg.MostUsedTools(10)
	if len(toolDefs) > 0 {
		for _, t := range toolDefs {
			usage := ""
			if s, ok := stats[t.Name]; ok && s.CallCount > 0 {
				usage = fmt.Sprintf(" (%d calls)", s.CallCount)
			}
			fmt.Fprintf(&sb, "  - %s: %s%s\n", t.Name, t.Description, usage)
		}
	}
	if c.skills != nil {
		skills := c.skills.List()
		if len(skills) > 0 {
			fmt.Fprintf(&sb, i18n.TL(i18n.KeyHelpSkillsAvail)+"\n", len(skills))
		}
	}
	io.WriteLine(sb.String())
}

func (c *Coordinator) printAgents(io transport.UserIO, args []string) {
	agents := c.pool.List()

	// Filter by agent name if argument provided
	filterName := ""
	if len(args) > 0 && args[0] != "" {
		filterName = args[0]
		// Show detailed info for a single agent
		for _, a := range agents {
			if a.Name == filterName {
				io.WriteLine(fmt.Sprintf("Agent:     %s", a.Name))
				io.WriteLine(fmt.Sprintf("Status:    %s", a.Status))
				io.WriteLine(fmt.Sprintf("Type:      %s", a.Kind))
				io.WriteLine(fmt.Sprintf("Role:      %s", a.Role))
				io.WriteLine(fmt.Sprintf("Tasks:     %d", a.TasksDone))
				io.WriteLine(fmt.Sprintf("Workspace: %s", a.Workspace))
				io.WriteLine(fmt.Sprintf("Session:   %s", a.SessionID))
				io.WriteLine(fmt.Sprintf("Task ID:   %s", a.CurrentTaskID))
				io.WriteLine(fmt.Sprintf("Tools:     %v", a.Tools))
				io.WriteLine("")
				return
			}
		}
		// Built-in agents check
		if c.buildinRegistry != nil {
			for _, ba := range c.buildinRegistry.List() {
				if ba.Name() == filterName {
					io.WriteLine(fmt.Sprintf("Agent:     %s", ba.Name()))
					io.WriteLine(fmt.Sprintf("Status:    active"))
					io.WriteLine(fmt.Sprintf("Type:      buildin"))
					io.WriteLine("")
					return
				}
			}
		}
		io.WriteLine(fmt.Sprintf("Agent %q not found.", filterName))
		io.WriteLine("")
		return
	}

	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyAgentHeader), "AGENT", "STATUS", "TYPE", "SESSION", "TASKS"))
	io.WriteLine("------------------------------------------------")
	for _, a := range agents {
		io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %-22s %d",
			a.Name, a.Status, a.Kind, a.SessionID, a.TasksDone))
	}

	// Built-in agents from registry
	if c.buildinRegistry != nil {
		for _, ba := range c.buildinRegistry.List() {
			io.WriteLine(fmt.Sprintf("%-16s %-10s %-6s %-12s %d",
				ba.Name(), "active", "buildin", "-", 0))
		}
	}

	if len(agents) == 0 && (c.buildinRegistry == nil || len(c.buildinRegistry.List()) == 0) {
		io.WriteLine(i18n.TL(i18n.KeyNoAgents))
		io.WriteLine(i18n.TL(i18n.KeyNoAgentsHint))
	}
	io.WriteLine("")
}

func (c *Coordinator) printMCP(io transport.UserIO) {
	toolDefs := c.agent.toolReg.List()
	if len(toolDefs) == 0 {
		io.WriteLine("No MCP tools loaded.")
		return
	}
	io.WriteLine(fmt.Sprintf("MCP tools (%d):", len(toolDefs)))
	for _, t := range toolDefs {
		src := ""
		if t.Source != "" {
			src = fmt.Sprintf(" [%s]", t.Source)
		}
		io.WriteLine(fmt.Sprintf("  %s%s - %s", t.Name, src, t.Description))
	}
	io.WriteLine("")
}

func (c *Coordinator) printCronTasks(io transport.UserIO) {
	if c.cronMgr == nil {
		io.WriteLine(i18n.TL(i18n.KeyCronNotAvail))
		return
	}
	tasks := c.cronMgr.List()
	if len(tasks) == 0 {
		io.WriteLine(i18n.TL(i18n.KeyNoCronTasks))
		io.WriteLine(i18n.TL(i18n.KeyNoCronHint))
		return
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCronHeader), "NAME", "SCHEDULE", "STATUS"))
	io.WriteLine("-----------------------------------------------------")
	for _, t := range tasks {
		status := "enabled"
		if !t.Enabled {
			status = "disabled"
		}
		io.WriteLine(fmt.Sprintf("%-20s %-12s %s", t.Name, t.Schedule, status))
	}
	results := c.cronMgr.PendingResults()
	if len(results) > 0 {
		io.WriteLine("")
		io.WriteLine(i18n.TL(i18n.KeyCronRecent))
		for _, r := range results {
			mark := "✓"
			if !r.Success {
				mark = "✗"
			}
			msg := r.Output
			if r.Error != "" {
				msg = r.Error
			}
			if len(msg) > 100 {
				msg = msg[:100] + "..."
			}
			io.WriteLine(fmt.Sprintf("  %s %s (%s): %s", mark, r.TaskName, r.CompletedAt.Format("15:04"), msg))
		}
	}
	io.WriteLine("")
}

func (c *Coordinator) handleModelCmd(args []string, io transport.UserIO) {
	fp, ok := c.agent.provider.(*provider.FailoverProvider)
	if !ok {
		io.WriteLine("Provider does not support failover.")
		return
	}

	if len(args) == 0 {
		cfgs := fp.Configs()
		if len(cfgs) == 0 {
			io.WriteLine("No providers configured")
			return
		}
		// List providers
		io.WriteLine("Available providers (type:model):")
		io.WriteLine("  " + fmt.Sprintf("%-20s %-30s %s", "NAME", "MODEL", "STATUS"))
		io.WriteLine("  " + strings.Repeat("-", 55))
		for i, pc := range cfgs {
			status := ""
			if i == fp.CurrentIndex() {
				status = "← active"
			}
			io.WriteLine("  " + fmt.Sprintf("%-20s %-30s %s", pc.Name, pc.Model, status))
		}
		io.WriteLine("")
		io.WriteLine("Use /model <name> to switch")
		return
	}

	// Switch to named provider
	name := args[0]
	if fp.SwitchTo(name) {
		cc := fp.CurrentConfig()
		c.agent.cfg.LLM.Type = cc.Type
		c.agent.cfg.LLM.BaseURL = cc.BaseURL
		c.agent.cfg.LLM.APIKey = cc.APIKey
		c.agent.cfg.LLM.Model = cc.Model
		c.agent.cfg.LLM.MaxTokens = cc.MaxTokens
		c.agent.rebuildCompressor()
		io.WriteLine(fmt.Sprintf("Switched to %s (%s)", name, c.agent.provider.Name()))
	} else {
		io.WriteLine(fmt.Sprintf("provider.Provider %q not found or unhealthy", name))
	}
}

func (c *Coordinator) handleCancelCmd(args []string, io transport.UserIO) {
	if len(args) > 0 {
		taskID := args[0]
		if c.pool.Cancel(taskID) {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCancelTask), taskID))
		} else {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyCancelNotFound), taskID))
		}
	} else {
		c.pool.CancelAll()
		io.WriteLine(i18n.TL(i18n.KeyCancelAll))
	}
}

func (c *Coordinator) handleStatus(sess *session.Session, state *LoopState, io transport.UserIO) {
	io.WriteLine(i18n.TL(i18n.KeyStatusHeader))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusProvider), c.agent.provider.Name()))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusModel), c.agent.cfg.LLM.Model))

	if sess != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusSession), sess.ID, state.Turn))
	} else {
		io.WriteLine(i18n.TL(i18n.KeyNoSession))
	}

	agents := c.pool.List()
	busy := 0
	for _, a := range agents {
		if a.Status == "busy" {
			busy++
		}
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusAgents), len(agents), busy))

	tools := c.agent.toolReg.List()
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusMCPTools), len(tools)))

	if c.skills != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusSkills), len(c.skills.List())))
	}
	if c.commands != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusCommands), len(c.commands.List())))
	}
	if c.cronMgr != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusCron), len(c.cronMgr.List())))
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyStatusMemory), m.Alloc/1024/1024))
	io.WriteLine("")
}

func (c *Coordinator) printContext(args []string, io transport.UserIO) {
	// Special keywords
	if len(args) > 0 && args[0] == "current" {
		c.printCurrentContext(io)
		return
	}
	if len(args) > 0 && args[0] == "system" {
		c.printSystemContext(io)
		return
	}

	// If a section name is given, display its content
	if len(args) > 0 {
		sectionName := args[0]
		if !strings.HasSuffix(sectionName, ".md") {
			sectionName += ".md"
		}
		content := c.agent.ctxBuilder.LoadSection(sectionName)
		if content == "" {
			io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSectionNF), sectionName))
			return
		}
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSectionHd)+"\n", strings.ToUpper(sectionName)))
		io.WriteLine(content)
		return
	}

	io.WriteLine(i18n.TL(i18n.KeyContextSummaryHd) + "\n")

	sessionID := "none"
	if c.currentSess != nil {
		sessionID = string(c.currentSess.ID)
	}
	io.WriteLine(fmt.Sprintf("Session:      %s", sessionID))
	io.WriteLine(fmt.Sprintf("Tokens:       in=%d [cache=%d miss=%d] out=%d", c.totalInputTokens, c.totalCachedTokens, c.totalMissedTokens, c.totalOutputTokens))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextProvider), c.agent.cfg.LLM.Model, c.agent.provider.Name()))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextConfigPath), len(configurablePaths)))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextMCPTools), len(c.agent.toolReg.List())))

	agents := c.pool.List()
	busyCount := 0
	for _, a := range agents {
		if a.Status == "busy" {
			busyCount++
		}
	}
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextAgents), len(agents), busyCount))

	if c.skills != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSkills), len(c.skills.List())))
	} else {
		io.WriteLine(i18n.TL(i18n.KeyContextSkillsNA))
	}
	if c.commands != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextCommands), len(c.commands.List())))
	} else {
		io.WriteLine(i18n.TL(i18n.KeyContextCommandsNA))
	}
	if c.cronMgr != nil {
		io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextCron), len(c.cronMgr.List())))
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	io.WriteLine(fmt.Sprintf("Memory:       %d MB used", m.Alloc/1024/1024))
	io.WriteLine(fmt.Sprintf(i18n.TL(i18n.KeyContextSelfEvolve), c.agent.cfg.Flags.SelfEvolution))
	io.WriteLine("")
	if loaded := c.agent.ctxBuilder.LoadedSections(); len(loaded) > 0 {
		io.WriteLine(i18n.TL(i18n.KeyContextSectionsHd))
		for _, s := range loaded {
			size := formatSize(s.Size)
			path := s.Path
			if path == "" {
				path = "(embedded)"
			}
			io.WriteLine(fmt.Sprintf("  %-20s %d  %s  %s", s.Name, s.Priority, size, path))
		}
		io.WriteLine("")
	}
}

func (c *Coordinator) printSystemContext(io transport.UserIO) {
	if c.lastLLMSystemPrompt == "" {
		io.WriteLine("(no LLM request has been made yet)")
		return
	}
	// Print in chunks to avoid overwhelming the terminal on a single line write.
	// Each chunk is written as a separate WriteLine for streaming transports.
	for _, chunk := range splitIntoChunks(c.lastLLMSystemPrompt, 4000) {
		io.WriteLine(chunk)
	}
}

func (c *Coordinator) printCurrentContext(io transport.UserIO) {
	io.WriteLine("=== System Prompt ===")
	if c.lastLLMSystemPrompt == "" {
		io.WriteLine("(no LLM request has been made yet)")
		return
	}
	io.WriteLine(c.lastLLMSystemPrompt)
	io.WriteLine("")

	io.WriteLine(fmt.Sprintf("=== Messages (%d) ===", len(c.lastLLMMessages)))
	for i, msg := range c.lastLLMMessages {
		io.WriteLine(fmt.Sprintf("--- [%d] %s ---", i, msg.Role))
		content := string(msg.Content)
		// Try to pretty-print JSON content
		if len(content) > 0 && content[0] == '[' {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(content), &blocks); err == nil {
				for _, block := range blocks {
					if txt, ok := block["text"]; ok {
						io.WriteLine(fmt.Sprintf("  text: %s", txt))
					} else if typ, ok := block["type"]; ok {
						io.WriteLine(fmt.Sprintf("  [type: %s]", typ))
					}
				}
			} else {
				io.WriteLine(content)
			}
		} else {
			io.WriteLine(content)
		}
		io.WriteLine("")
	}
}

func (c *Coordinator) printTransport(io transport.UserIO) {
	tc := c.agent.cfg.Transport
	io.WriteLine("Transports:")
	if tc.Stdio.Enabled {
		io.WriteLine("  stdio     enabled")
	}
	if tc.SSH.Enabled {
		addr := tc.SSH.Addr
		if addr == "" {
			addr = ":2222"
		}
		io.WriteLine(fmt.Sprintf("  ssh       %s", addr))
	}
	if tc.MQTT.Enabled {
		io.WriteLine(fmt.Sprintf("  mqtt      %s", tc.MQTT.Broker))
	}
	if tc.Email.Enabled {
		io.WriteLine(fmt.Sprintf("  email     %s (IMAP:%s:%d)", tc.Email.Username, tc.Email.IMAPHost, tc.Email.IMAPPort))
	}
	if tc.DingTalk.Enabled {
		io.WriteLine("  dingtalk  enabled")
	}
	if !tc.Stdio.Enabled && !tc.SSH.Enabled && !tc.MQTT.Enabled && !tc.Email.Enabled && !tc.DingTalk.Enabled {
		io.WriteLine("  (none enabled)")
	}
}

func (c *Coordinator) handleConfigListCmd(io transport.UserIO) {
	var lastSection string
	for _, entry := range configurablePaths {
		section := sectionOf(entry.path)
		if section != lastSection {
			if lastSection != "" {
				io.WriteLine("")
			}
			io.WriteLine(fmt.Sprintf("── %s ──", section))
			lastSection = section
		}
		val := entry.get(c.agent.cfg)
		io.WriteLine(fmt.Sprintf("  %s = %v", entry.path, formatValue(val)))
		io.WriteLine(fmt.Sprintf("    %s", entry.description))
	}
	io.WriteLine("")
	io.WriteLine("Use /config get <path> to read a value, /config set <path> <value> to modify.")
}

func (c *Coordinator) handleConfigGetCmd(args []string, io transport.UserIO) {
	if len(args) == 0 {
		io.WriteLine("Usage: /config get <path>")
		io.WriteLine("Example: /config get llm.temperature")
		io.WriteLine("Use /config to see all available paths.")
		return
	}
	path := args[0]
	entry := findConfigEntry(path)
	if entry == nil {
		children := findConfigChildren(path)
		if len(children) == 0 {
			io.WriteLine(fmt.Sprintf("Unknown path %q — use /config to see all paths", path))
			return
		}
		for _, ch := range children {
			val := ch.get(c.agent.cfg)
			io.WriteLine(fmt.Sprintf("%s = %v", ch.path, formatValue(val)))
			io.WriteLine(fmt.Sprintf("  %s", ch.description))
		}
		return
	}
	val := entry.get(c.agent.cfg)
	io.WriteLine(fmt.Sprintf("%s = %v", entry.path, formatValue(val)))
	io.WriteLine(fmt.Sprintf("  %s", entry.description))
}

func (c *Coordinator) handleConfigSetCmd(args []string, io transport.UserIO) {
	if len(args) < 2 {
		io.WriteLine("Usage: /config set <path> <value>")
		io.WriteLine("Example: /config set llm.temperature 0.8")
		io.WriteLine("Use /config to see all available paths.")
		return
	}
	path := args[0]
	valueStr := args[1]

	entry := findConfigEntry(path)
	if entry == nil {
		io.WriteLine(fmt.Sprintf("Unknown path %q — use /config to see all paths", path))
		return
	}

	oldVal := entry.get(c.agent.cfg)
	coerced, err := coerceValue(valueStr, oldVal)
	if err != nil {
		io.WriteLine(fmt.Sprintf("Invalid value for %s: %v", path, err))
		return
	}

	if err = entry.set(c.agent.cfg, coerced); err != nil {
		io.WriteLine(fmt.Sprintf("Failed to set %s: %v", path, err))
		return
	}

	if entry.needsSync {
		c.agent.rebuildCompressor()
		zap.S().Infow("config changed: compressor rebuilt", "path", path)
	}

	filePath := filepath.Join(config.ProjectConfigDir, config.ConfigFileName+".yaml")
	existing := make(map[string]any)
	if data, readErr := os.ReadFile(filePath); readErr == nil {
		if err = yaml.Unmarshal(data, &existing); err != nil {
			zap.S().Warnw("failed to parse existing config", "path", filePath, "error", err)
		}
	}
	overlayConfig(existing, c.agent.cfg)
	data, err := yaml.Marshal(existing)
	if err != nil {
		io.WriteLine(fmt.Sprintf("Set %s = %v (was: %v). WARN: failed to persist: %v", path, formatValue(coerced), formatValue(oldVal), err))
		return
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0700); err != nil {
		io.WriteLine(fmt.Sprintf("Set %s = %v (was: %v). WARN: failed to create config dir: %v", path, formatValue(coerced), formatValue(oldVal), err))
		return
	}
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		io.WriteLine(fmt.Sprintf("Set %s = %v (was: %v). WARN: failed to write config: %v", path, formatValue(coerced), formatValue(oldVal), err))
		return
	}

	io.WriteLine(fmt.Sprintf("Set %s = %v (was: %v) and saved to %s", path, formatValue(coerced), formatValue(oldVal), filePath))
}

func coerceValue(s string, current any) (any, error) {
	switch current.(type) {
	case bool:
		return strconv.ParseBool(s)
	case int:
		return strconv.Atoi(s)
	case float64:
		return strconv.ParseFloat(s, 64)
	case string:
		return s, nil
	case []string:
		if s == "" || s == "[]" {
			return []string{}, nil
		}
		return strings.Split(s, ","), nil
	default:
		return s, nil
	}
}

func formatSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func (c *Coordinator) handleFeedback(args []string, io transport.UserIO) {
	if len(args) == 0 {
		io.WriteLine("Usage: /feedback <message>  —  Send feedback to the development team")
		io.WriteLine("Please provide your feedback text as arguments.")
		return
	}
	ecfg := c.agent.cfg.Transport.Email
	if ecfg.SMTPHost == "" {
		io.WriteLine("Email SMTP not configured. Set transport.email in config.yaml to send feedback.")
		return
	}
	msg := strings.Join(args, " ")
	subject := "[feedback]" + msg
	from := ecfg.From
	if from == "" {
		from = ecfg.Username
	}
	to := "feedback@siciv.space"
	host := ecfg.SMTPHost
	port := ecfg.SMTPPort
	if port <= 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	var rawMsg strings.Builder
	fmt.Fprintf(&rawMsg, "From: %s\r\n", from)
	fmt.Fprintf(&rawMsg, "To: %s\r\n", to)
	fmt.Fprintf(&rawMsg, "Subject: %s\r\n", subject)
	fmt.Fprintf(&rawMsg, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	rawMsg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	rawMsg.WriteString("\r\n")
	rawMsg.WriteString(msg)
	io.WriteLine(fmt.Sprintf("Sending feedback to %s...", to))
	if ecfg.UseTLS && port == 465 {
		if err := sendFeedbackTLS(addr, host, from, to, rawMsg.String(), ecfg.Username, ecfg.Password); err != nil {
			io.WriteLine(fmt.Sprintf("Failed to send feedback: %v", err))
			return
		}
	} else {
		if err := sendFeedbackPlain(addr, host, from, to, rawMsg.String(), ecfg.Username, ecfg.Password); err != nil {
			io.WriteLine(fmt.Sprintf("Failed to send feedback: %v", err))
			return
		}
	}
	io.WriteLine("Thank you! Your feedback has been sent to the development team.")
}

func sendFeedbackPlain(addr, host, from, to, msg, user, pass string) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()
	if err = c.Hello(host); err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	if user != "" || pass != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err = c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}
	if err = c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err = c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return c.Quit()
}

func sendFeedbackTLS(addr, host, from, to, msg, user, pass string) error {
	tconn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("connect TLS: %w", err)
	}
	defer tconn.Close()
	c, err := smtp.NewClient(tconn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()
	if user != "" || pass != "" {
		if ok, _ := c.Extension("AUTH"); ok {
			if err = c.Auth(smtp.PlainAuth("", user, pass, host)); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}
	if err = c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err = c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err = w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return c.Quit()
}

func (c *Coordinator) handleNew(sess *session.Session, state *LoopState, io transport.UserIO) {
	oldTurns := state.Turn
	oldID := sess.ID

	// Generate summary and close the old session
	c.agent.generateSummary(sess, state)
	sess.Close()
	c.agent.sessMgr.Remove(sess.ID)

	// Create a new child session
	newSess, err := c.agent.sessMgr.NewSession(c.agent.cfg.Session.MaxLoop)
	if err != nil {
		io.WriteLine(fmt.Sprintf("Failed to create new session: %v", err))
		return
	}
	newSess.LogSystem(fmt.Sprintf("fresh start from session %s (turn %d)", oldID, oldTurns))

	// Reset state with new session, preserving config and transport
	state.Sess = newSess
	state.Turn = 0
	state.Messages = nil
	state.ToolCallCount = 0
	state.ErrorCount = 0
	state.StopReason = ""
	state.SummaryGenerated = false
	c.currentSess = newSess

	zap.S().Infow("session reset via /new",
		"old_session", oldID,
		"new_session", newSess.ID,
	)

	io.WriteLine(fmt.Sprintf("New session %s started. Previous: %s (%d turns)",
		newSess.ID, oldID, oldTurns,
	))
	io.WriteLine("")
}

// splitIntoChunks splits a string into chunks of max N lines each,
// preserving the content for display on streaming transports.
func splitIntoChunks(s string, maxLines int) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return []string{s}
	}
	var chunks []string
	for i := 0; i < len(lines); i += maxLines {
		end := i + maxLines
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, strings.Join(lines[i:end], "\n"))
	}
	return chunks
}
