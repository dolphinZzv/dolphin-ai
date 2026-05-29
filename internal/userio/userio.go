package userio

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"dolphin/internal/agentio"
	"dolphin/internal/command"
	"dolphin/internal/session"
	"dolphin/internal/transport"
)

type UserIO struct {
	agentIO    *agentio.AgentIO
	cmdReg     *command.Registry
	sessionMgr *session.Manager
	mu         sync.Mutex
	sessions   map[string]*session.Session // per-transport session
}

func NewUserIO(a *agentio.AgentIO, cmdReg *command.Registry, mgr *session.Manager) *UserIO {
	return &UserIO{
		agentIO:    a,
		cmdReg:     cmdReg,
		sessionMgr: mgr,
		sessions:   make(map[string]*session.Session),
	}
}

// Handle processes user input. Returns true if a turn was queued.
// Each transport gets its own dedicated session (no cross-transport interference).
func (u *UserIO) Handle(ctx context.Context, tio transport.IO, input string) bool {
	if strings.HasPrefix(input, "/") {
		line := strings.TrimPrefix(input, "/")
		out := u.cmdReg.Execute(line)

		// If the command created a new session, update the per-transport session.
		if line == "session new" {
			u.mu.Lock()
			sess := u.sessionMgr.Active()
			u.sessions[tio.ID()] = sess
			u.mu.Unlock()
		}

		// Send command output back to the transport.
		if out != "" {
			_ = tio.Write(ctx, out)
		}
		return false
	}

	u.mu.Lock()
	sess, ok := u.sessions[tio.ID()]
	if !ok {
		sess = u.sessionMgr.NewSession(ctx)
		u.sessions[tio.ID()] = sess
	}
	u.mu.Unlock()

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
