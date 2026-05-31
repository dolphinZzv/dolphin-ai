package logger

import (
	"os"

	"dolphin/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func New(cfg *config.Config) *zap.Logger {
	level := parseLevel(cfg.GetString("log.level"))
	filePath := cfg.GetString("log.file")

	var ws zapcore.WriteSyncer
	if filePath != "" {
		ws = zapcore.AddSync(&lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    cfg.GetInt("log.max_size"),
			MaxBackups: cfg.GetInt("log.max_backups"),
			MaxAge:     cfg.GetInt("log.max_age"),
			Compress:   cfg.GetBool("log.compress"),
		})
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

	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
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
