package logger

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"dolphin/internal/config"
)

func New(cfg *config.Config) *zap.Logger {
	level := parseLevel(cfg.GetString("log.level"))
	filePath := cfg.GetString("log.file")

	var ws zapcore.WriteSyncer
	var lj *lumberjack.Logger
	if filePath != "" {
		lj = &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    cfg.GetInt("log.max_size"),
			MaxBackups: cfg.GetInt("log.max_backups"),
			MaxAge:     cfg.GetInt("log.max_age"),
			Compress:   cfg.GetBool("log.compress"),
		}
		ws = zapcore.AddSync(lj)
	}
	if ws == nil {
		ws = zapcore.AddSync(os.Stdout)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		ws,
		level,
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	// Time-based rotation
	if interval := cfg.GetDuration("log.rotate_interval"); interval > 0 && lj != nil {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				_ = lj.Rotate()
			}
		}()
	}

	return logger
}

func parseLevel(level string) zapcore.LevelEnabler {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func Named(root *zap.Logger, name string) *zap.Logger {
	if name == "" {
		return root
	}
	return root.Named(name)
}
