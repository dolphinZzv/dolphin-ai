package setup

import (
	"context"

	"go.uber.org/zap"

	"dolphin/internal/command"
	"dolphin/internal/dream"
)

// DreamBootstrapper creates the Dream service and registers its commands.
type DreamBootstrapper struct{}

func (b *DreamBootstrapper) Name() string { return "dream" }
func (b *DreamBootstrapper) Index() int   { return 93 }

func (b *DreamBootstrapper) Bootstrap(ctx context.Context, c *Context) error {
	if c.Dream != nil {
		return nil
	}

	cfg := dream.Config{
		Enabled:             c.Config.GetBool("dream.enabled"),
		IdleMinutes:         c.Config.GetInt("dream.idle_minutes"),
		ExitIdleMinutes:     c.Config.GetInt("dream.exit_idle_minutes"),
		AutoApply:           c.Config.GetBool("dream.auto_apply"),
		MinSessions:         c.Config.GetInt("dream.min_sessions"),
		MinUserMessages:     c.Config.GetInt("dream.min_user_messages"),
		MaxConsecutiveEmpty: c.Config.GetInt("dream.max_consecutive_empty"),
		MinImpactThreshold:  c.Config.GetFloat("dream.min_impact_threshold"),
		FileCooldownDreams:  c.Config.GetInt("dream.file_cooldown_dreams"),
		MaxEditsPerDream:    c.Config.GetInt("dream.max_edits_per_dream"),
		CalibrationWindow:   c.Config.GetInt("dream.calibration_window"),
		CalibrationMinStep:  c.Config.GetFloat("dream.calibration_min_step"),
		CalibrationFloor:    c.Config.GetFloat("dream.calibration_confidence_floor"),
		CalibrationCeiling:  c.Config.GetFloat("dream.calibration_confidence_ceiling"),
		ReflectModel:        c.Config.GetString("dream.reflect_model"),
		MaxReflectTokens:    c.Config.GetInt("dream.max_reflect_tokens"),
	}

	if !cfg.Enabled {
		// Dream is disabled — still register commands, but no service.
		c.Logger.Info("dream disabled by config")
		return nil
	}

	d := dream.New(
		cfg,
		c.Mem,
		c.SessionMgr,
		c.Brain,
		c.LLMProvider,
		c.AgentIO,
		c.Logger.Named("dream"),
	)

	c.Dream = d
	command.RegisterDream(c.CmdReg, d)

	c.Logger.Info("dream bootstrapper loaded",
		zap.Int("idle_minutes", cfg.IdleMinutes),
		zap.Bool("auto_apply", cfg.AutoApply),
	)
	return nil
}
