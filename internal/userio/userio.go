package userio

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/brain"
	"dolphin/internal/command"
	"dolphin/internal/i18n"
	"dolphin/internal/session"
	"dolphin/internal/transport"
)

// interactiveRunner allows transports to run interactive commands that need terminal control.
type interactiveRunner interface {
	RunInteractive(ctx context.Context, name string, args ...string) error
}

type UserIO struct {
	agentIO     *agentio.AgentIO
	cmdReg      *command.Registry
	brain       *brain.Brain
	sessionMgr  *session.Manager
	sessionMode string // "per_transport" or "shared"
}

func NewUserIO(a *agentio.AgentIO, cmdReg *command.Registry, b *brain.Brain, mgr *session.Manager, sessionMode string) *UserIO {
	return &UserIO{
		agentIO:     a,
		cmdReg:      cmdReg,
		brain:       b,
		sessionMgr:  mgr,
		sessionMode: sessionMode,
	}
}

// Handle processes user input. Returns true if a turn was queued.
// Interactive transports (stdio) reuse the same session across messages.
// Non-interactive transports (email) create a new session per message.
func (u *UserIO) Handle(ctx context.Context, tio transport.IO, input transport.Input) bool {
	if strings.HasPrefix(input.Text, "/") {
		line := strings.TrimPrefix(input.Text, "/")
		words := strings.Fields(line)

		// If not a built-in command, try brain script, then system command.
		if len(words) > 0 && !u.cmdReg.HasCommand(words[0]) {
			// Try brain script.
			if u.brain != nil {
				s, err := brain.ReadScript(ctx, u.brain, words[0])
				if err == nil {
					if !s.Enabled {
						_ = tio.Write(ctx, fmt.Sprintf("script %q is disabled\n", words[0]))
						return false
					}
					if s.Content == "" {
						_ = tio.Write(ctx, fmt.Sprintf("script %q has no content\n", words[0]))
						return false
					}
					sess := tio.NewSession(ctx)
					if sess == nil {
						sess = u.sessionMgr.NewSession(ctx)
					}
					ctx = transport.WithInfo(ctx, &transport.Info{ID: tio.ID()})
					u.agentIO.SendTurn(ctx, &agentio.Turn{
						TransportID: tio.ID(),
						SessionID:   sess.ID,
						Input:       s.Content,
						Context:     tio.Context(),
					})
					return true
				}
			}

			// Try system command.
			if _, lookErr := exec.LookPath(words[0]); lookErr == nil {
				if isInteractiveCmd(words[0], words[1:]) {
					// Interactive — hand terminal over to child process.
					if runner, ok := tio.(interactiveRunner); ok {
						_ = runner.RunInteractive(ctx, words[0], words[1:]...)
						return false
					}
					// Transport cannot run interactive, fall through to LLM.
				} else {
					// Non-interactive — capture output.
					cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					cmd := exec.CommandContext(cmdCtx, words[0], words[1:]...) //nolint:gosec // G204: runs a configured editor/pager command
					output, _ := cmd.CombinedOutput()
					outStr := string(output)
					const maxOutput = 64 * 1024
					if len(outStr) > maxOutput {
						outStr = outStr[:maxOutput] + "\n... (output truncated)"
					}
					_ = tio.Write(ctx, outStr)
					return false
				}
			}

			// Not found anywhere — send to LLM for analysis.
			sess := tio.NewSession(ctx)
			if sess == nil {
				sess = u.sessionMgr.NewSession(ctx)
			}
			ctx = transport.WithInfo(ctx, &transport.Info{ID: tio.ID()})
			u.agentIO.SendTurn(ctx, &agentio.Turn{
				TransportID: tio.ID(),
				SessionID:   sess.ID,
				Input:       fmt.Sprintf(i18n.T("userio.script_not_found"), words[0]),
				Context:     tio.Context(),
			})
			return true
		}

		cmdCtx := transport.WithInfo(ctx, &transport.Info{ID: tio.ID()})
		out := u.cmdReg.Execute(cmdCtx, line, tio.Capability().RenderTextMarkdown)
		if out != "" {
			_ = tio.Write(ctx, out+"\n")
		}
		if line == "session new" || line == "new" || line == "clear" {
			if ss, ok := tio.(interface{ SetSession(*session.Session) }); ok {
				ss.SetSession(u.sessionMgr.Active())
			}
		}
		if strings.HasPrefix(line, "models use ") {
			if notifier, ok := tio.(interface{ NotifyModelChange(string) }); ok {
				name := strings.TrimSpace(strings.TrimPrefix(line, "models use "))
				notifier.NotifyModelChange(name)
			}
		}
		return false
	}

	sess := tio.NewSession(ctx)
	if sess == nil {
		sess = u.sessionMgr.NewSession(ctx)
	}
	// Store transport-level user metadata in session data.
	// In shared mode, skip session.Data to avoid cross-transport overwrites.
	if u.sessionMode != "shared" {
		if m, ok := tio.(interface {
			UserID() string
			UserNick() string
		}); ok {
			if uid := m.UserID(); uid != "" {
				sess.Set("user_id", uid)
			}
			if nick := m.UserNick(); nick != "" {
				sess.Set("user_nick", nick)
			}
		}
		if c, ok := tio.(interface{ ConversationID() string }); ok {
			if cid := c.ConversationID(); cid != "" {
				sess.Set("conversation_id", cid)
			}
		}
	}
	sendFn := u.agentIO.SendTurn
	if pt, ok := tio.(interface{ IsPriority() bool }); ok && pt.IsPriority() {
		sendFn = u.agentIO.SendTurnPriority
		if rst, ok := tio.(interface{ ResetPriority() }); ok {
			defer rst.ResetPriority()
		}
	}
	sendFn(ctx, &agentio.Turn{
		TransportID: tio.ID(),
		SessionID:   sess.ID,
		Input:       input.Text,
		Parts:       input.Parts,
		Context:     tio.Context(),
	})
	return true
}

// ReadLine reads a single line of input from the transport.
func (u *UserIO) ReadLine(ctx context.Context, tio transport.IO) (string, error) {
	in, err := tio.Read(ctx)
	if err != nil {
		return "", err
	}
	return in.Text, nil
}

// WriteLine writes text followed by a newline to the transport.
func (u *UserIO) WriteLine(ctx context.Context, tio transport.IO, text string) error {
	if err := tio.Write(ctx, text); err != nil {
		return err
	}
	return tio.Flush()
}

// isInteractiveCmd returns true for commands that run interactively and never exit on their own.
func isInteractiveCmd(name string, args []string) bool {
	switch name {
	case "top", "htop", "btop", "iotop", "iftop",
		"less", "more", "most",
		"vim", "vi", "nvim", "nano", "emacs", "micro",
		"ssh", "telnet", "nc", "ncat",
		"fzf",
		"hexdump", "xxd", "watch":
		return true
	case "python", "python3", "node", "irb", "bash", "sh", "zsh":
		return len(args) == 0
	}
	return false
}
