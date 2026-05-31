package userio

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"dolphin/internal/agentio"
	"dolphin/internal/command"
	"dolphin/internal/session"
	"dolphin/internal/transport"
)

type UserIO struct {
	agentIO    *agentio.AgentIO
	cmdReg     *command.Registry
	sessionMgr *session.Manager
}

func NewUserIO(a *agentio.AgentIO, cmdReg *command.Registry, mgr *session.Manager) *UserIO {
	return &UserIO{
		agentIO:    a,
		cmdReg:     cmdReg,
		sessionMgr: mgr,
	}
}

// Handle processes user input. Returns true if a turn was queued.
// Interactive transports (stdio) reuse the same session across messages.
// Non-interactive transports (email) create a new session per message.
func (u *UserIO) Handle(ctx context.Context, tio transport.IO, input string) bool {
	if strings.HasPrefix(input, "/") {
		line := strings.TrimPrefix(input, "/")
		out := u.cmdReg.Execute(line, tio.Capability().RenderTextMarkdown)
		if out != "" {
			_ = tio.Write(ctx, out)
		}
		if line == "session new" {
			if ss, ok := tio.(interface{ SetSession(*session.Session) }); ok {
				ss.SetSession(u.sessionMgr.Active())
			}
		}
		return false
	}

	sess := tio.NewSession(ctx)
	if sess == nil {
		sess = u.sessionMgr.NewSession(ctx)
	}
	// Store transport-level user metadata in session data.
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
	u.agentIO.SendTurn(ctx, &agentio.Turn{
		TransportID: tio.ID(),
		SessionID:   sess.ID,
		Input:       input,
		Context:     tio.Context(),
	})
	return true
}

// ReadLine reads a single line of input from the transport.
func (u *UserIO) ReadLine(ctx context.Context, tio transport.IO) (string, error) {
	return tio.Read(ctx)
}

// WriteLine writes text followed by a newline to the transport.
func (u *UserIO) WriteLine(ctx context.Context, tio transport.IO, text string) error {
	if err := tio.Write(ctx, text); err != nil {
		return err
	}
	return tio.Flush()
}

// ReadPassword reads input without echoing (requires interactive transport).
func (u *UserIO) ReadPassword(ctx context.Context, tio transport.IO, prompt string) (string, error) {
	cap := tio.Capability()
	if !cap.Interactive {
		return "", fmt.Errorf("transport %s does not support password input", tio.ID())
	}
	if err := tio.Write(ctx, prompt); err != nil {
		return "", err
	}
	return tio.Read(ctx)
}

func (u *UserIO) Confirm(ctx context.Context, tio transport.IO, msg string) (bool, error) {
	cap := tio.Capability()
	if !cap.Interactive {
		return false, fmt.Errorf("transport %s does not support interactive confirm", tio.ID())
	}

	if err := tio.Write(ctx, msg+" (y/n): "); err != nil {
		return false, err
	}

	answer, err := tio.Read(ctx)
	if err != nil {
		return false, err
	}

	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

func (u *UserIO) Select(ctx context.Context, tio transport.IO, opts []string) (int, error) {
	cap := tio.Capability()
	if !cap.Interactive {
		return 0, fmt.Errorf("transport %s does not support interactive select", tio.ID())
	}

	for i, opt := range opts {
		if err := tio.Write(ctx, fmt.Sprintf("%d. %s", i+1, opt)); err != nil {
			return 0, err
		}
	}

	answer, err := tio.Read(ctx)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(answer))
}
