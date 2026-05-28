package logger

import (
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// atomicLevel is the global atomic level for runtime log level changes.
// Initialized by Init; nil before first call.
var atomicLevel *zap.AtomicLevel

// Config holds logger configuration.
type Config struct {
	Level     string // debug, info, warn, error
	File      string // log file path (empty = stderr only)
	MaxSize   int    // megabytes before rotation (default 100)
	MaxAge    int    // days to retain old files (default 30)
	MaxBackup int    // max old files to retain (default 3)
}

// Init sets up the global zap logger with optional lumberjack rotation.
// The log level can be changed at runtime via SetLevel.
func Init(cfg Config) {
	level := parseLevel(cfg.Level)
	lvl := zap.NewAtomicLevelAt(level)
	atomicLevel = &lvl

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderCfg),
		getSyncer(cfg),
		atomicLevel,
	)

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	zap.ReplaceGlobals(logger)
}

// SetLevel changes the global log level at runtime. Accepts "debug", "info",
// "warn", "warning", "error". Invalid values are silently ignored.
func SetLevel(level string) {
	if atomicLevel == nil {
		return
	}
	atomicLevel.SetLevel(parseLevel(level))
}

func getSyncer(cfg Config) zapcore.WriteSyncer {
	if cfg.File == "" {
		return zapcore.AddSync(os.Stderr)
	}

	dir := filepath.Dir(cfg.File)
	if dir != "." {
		if err := os.MkdirAll(dir, 0700); err != nil {
			zap.S().Warnw("mkdir log dir failed", "error", err, "dir", dir)
		}
	}

	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = 100
	}
	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = 30
	}
	maxBackup := cfg.MaxBackup
	if maxBackup <= 0 {
		maxBackup = 3
	}

	lumber := &lumberjack.Logger{
		Filename:   cfg.File,
		MaxSize:    maxSize,
		MaxAge:     maxAge,
		MaxBackups: maxBackup,
		LocalTime:  true,
		Compress:   true,
	}

	return zapcore.AddSync(lumber)
}

func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
