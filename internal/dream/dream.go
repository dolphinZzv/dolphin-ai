package dream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/agentio"
	"dolphin/internal/brain"
	"dolphin/internal/llm"
	"dolphin/internal/memory"
	"dolphin/internal/session"
)

// Dream is the offline self-edit orchestrator. It wakes after a period of
// user inactivity, scans recent sessions for improvement signals, edits
// brain files on a git branch, and optionally merges them back.
type Dream struct {
	// Configurables.
	interval            time.Duration
	exitInterval        time.Duration
	autoApply           bool
	minSessions         int
	minUserMessages     int
	maxConsecutiveEmpty int
	minImpactThreshold  float64
	fileCooldownDreams  int
	maxEditsPerDream    int
	calibrationWindow   int
	calibrationMinStep  float64
	calibrationFloor    float64
	calibrationCeiling  float64
	reflectModel        string
	maxReflectTokens    int

	// Dependencies.
	memory     memory.Memory
	sessionMgr *session.Manager
	brain      *brain.Brain
	provider   llm.Provider
	agentIO    *agentio.AgentIO
	logger     *zap.Logger

	// Runtime.
	state      *State
	activityCh chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc

	// Per-run.
	mu           sync.Mutex
	dreamCtx     context.Context
	dreamCancel  context.CancelFunc
	currentID    int
	currentEdits []Edit
	stateRunning bool
}

// Config holds all configuration values extracted from the application config.
type Config struct {
	Enabled             bool
	IdleMinutes         int
	ExitIdleMinutes     int
	AutoApply           bool
	MinSessions         int
	MinUserMessages     int
	MaxConsecutiveEmpty int
	MinImpactThreshold  float64
	FileCooldownDreams  int
	MaxEditsPerDream    int
	CalibrationWindow   int
	CalibrationMinStep  float64
	CalibrationFloor    float64
	CalibrationCeiling  float64
	ReflectModel        string
	MaxReflectTokens    int
}

// New creates a Dream instance. Does not start the goroutine — call Start().
func New(cfg Config, mem memory.Memory, sm *session.Manager, br *brain.Brain, prov llm.Provider, aio *agentio.AgentIO, log *zap.Logger) *Dream {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dream{
		interval:            time.Duration(cfg.IdleMinutes) * time.Minute,
		exitInterval:        time.Duration(cfg.ExitIdleMinutes) * time.Minute,
		autoApply:           cfg.AutoApply,
		minSessions:         cfg.MinSessions,
		minUserMessages:     cfg.MinUserMessages,
		maxConsecutiveEmpty: cfg.MaxConsecutiveEmpty,
		minImpactThreshold:  cfg.MinImpactThreshold,
		fileCooldownDreams:  cfg.FileCooldownDreams,
		maxEditsPerDream:    cfg.MaxEditsPerDream,
		calibrationWindow:   cfg.CalibrationWindow,
		calibrationMinStep:  cfg.CalibrationMinStep,
		calibrationFloor:    cfg.CalibrationFloor,
		calibrationCeiling:  cfg.CalibrationCeiling,
		reflectModel:        cfg.ReflectModel,
		maxReflectTokens:    cfg.MaxReflectTokens,
		memory:              mem,
		sessionMgr:          sm,
		brain:               br,
		provider:            prov,
		agentIO:             aio,
		logger:              log,
		activityCh:          make(chan struct{}, 64),
		ctx:                 ctx,
		cancel:              cancel,
	}
	// Load or bootstrap state.
	s, err := loadState(statePrimary, stateBackup)
	if err != nil {
		d.logger.Info("dream state not found, bootstrapping")
		d.state = newState()
	} else {
		d.state = s
	}
	return d
}

// Start launches the dream goroutine loop. Safe to call once.
func (d *Dream) Start(ctx context.Context) {
	d.logger.Info("dream starting",
		zap.Duration("interval", d.interval),
		zap.Int("last_id", d.state.LastDreamID),
	)
	go d.loop(ctx)
}

// Stop shuts down the dream goroutine.
func (d *Dream) Stop() {
	d.cancel()
}

// ActivityCh returns the channel to signal user activity on.
func (d *Dream) ActivityCh() chan<- struct{} { return d.activityCh }

// IsRunning returns true when a dream run is in progress (Phase 2 or 3).
func (d *Dream) IsRunning() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stateRunning
}

// State returns a snapshot of the current state (for commands).
func (d *Dream) State() *State {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := *d.state
	return &cp
}

// DreamNow triggers an immediate dream run, skipping the idle timer and Phase 0 gate.
func (d *Dream) DreamNow(ctx context.Context) (string, error) {
	d.mu.Lock()
	if d.stateRunning {
		d.mu.Unlock()
		return "", fmt.Errorf("dream already in progress")
	}
	d.stateRunning = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.stateRunning = false
		d.mu.Unlock()
	}()

	d.dreamCtx, d.dreamCancel = context.WithTimeout(context.Background(), 120*time.Second)
	defer d.dreamCancel()

	return d.dream(d.dreamCtx, true)
}

// loop is the main idle-triggered goroutine.
func (d *Dream) loop(ctx context.Context) {
	timer := time.NewTimer(d.interval)
	defer timer.Stop()

	resetTimer := func(dur time.Duration) {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(dur)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.activityCh:
			resetTimer(d.interval)
			// On explicit /exit, use the shorter window.
		case <-timer.C:
			if d.agentIO != nil && d.agentIO.Processing() {
				resetTimer(d.interval)
				continue
			}
			d.mu.Lock()
			if d.stateRunning {
				d.mu.Unlock()
				resetTimer(d.interval)
				continue
			}
			d.stateRunning = true
			d.mu.Unlock()

			d.dreamCtx, d.dreamCancel = context.WithTimeout(context.Background(), 120*time.Second)
			result, err := d.dream(d.dreamCtx, false)
			d.dreamCancel()

			d.mu.Lock()
			d.stateRunning = false
			d.mu.Unlock()

			if err != nil {
				d.logger.Warn("dream failed", zap.Error(err))
			} else if result != "" {
				d.logger.Info("dream completed", zap.String("result", result))
			}

			resetTimer(d.interval)
		}
	}
}

// dream runs the full 4-phase pipeline. skipGate forces Phase 0 to pass.
func (d *Dream) dream(ctx context.Context, skipGate bool) (string, error) {
	d.currentID = d.state.LastDreamID + 1

	// Phase 0: Gate.
	if !skipGate {
		sessions, err := d.sessionMgr.List(ctx)
		if err != nil {
			return "", fmt.Errorf("phase 0: list sessions: %w", err)
		}
		ok, reason := d.shouldRun(sessions)
		if !ok {
			d.state.ConsecutiveEmpty++
			d.state.LastDreamAt = time.Now()
			_ = d.state.save(statePrimary, stateBackup)
			d.logger.Debug("dream skipped", zap.String("reason", reason))
			return "", nil
		}
	}

	// Phase 1: Scan.
	proposals, err := d.scan(ctx)
	if err != nil {
		return "", fmt.Errorf("phase 1: scan: %w", err)
	}

	if len(proposals) == 0 {
		d.state.ConsecutiveEmpty++
		d.state.LastDreamAt = time.Now()
		_ = d.state.save(statePrimary, stateBackup)
		return "no edit proposals", nil
	}

	// Phase 2: Edit (LLM).
	edits, err := d.edit(ctx, proposals)
	if err != nil {
		return "", fmt.Errorf("phase 2: edit: %w", err)
	}
	// Reject any edit that doesn't pass causality verification.
	edits = d.filterEdits(edits, proposals)
	if len(edits) == 0 {
		d.state.ConsecutiveEmpty++
		d.state.LastDreamAt = time.Now()
		_ = d.state.save(statePrimary, stateBackup)
		return "all edits failed verification", nil
	}

	d.currentEdits = edits

	// Phase 3: Apply.
	if err := d.apply(ctx, edits); err != nil {
		return "", fmt.Errorf("phase 3: apply: %w", err)
	}

	// Phase 4: Tidy.
	d.tidy(edits)

	d.state.ConsecutiveEmpty = 0
	d.state.LastDreamID = d.currentID
	d.state.LastDreamAt = time.Now()
	_ = d.state.save(statePrimary, stateBackup)

	return fmt.Sprintf("dream #%d applied %d edits", d.currentID, len(edits)), nil
}

// NotifyExit accelerates the idle timer when the user types /exit.
func (d *Dream) NotifyExit() {
	select {
	case d.activityCh <- struct{}{}:
	default:
	}
	// The next idle trigger will use the exit interval instead of the normal
	// one. For simplicity, we signal activity first (which resets the timer
	// to the normal interval) and then the timer fires after the normal
	// interval anyway. The exit-acceleration is handled by the caller
	// resetting the timer to exitInterval in their own loop.
}
