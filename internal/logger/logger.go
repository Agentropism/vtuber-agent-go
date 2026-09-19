package logger

import (
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func ProvideLogger(cfg *config.Config) (*zap.Logger, error) {
	logCfg := zap.NewProductionConfig()
	logCfg.Level = zap.NewAtomicLevelAt(parseLevel(cfg.Log.Level))
	return logCfg.Build()
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zap.DebugLevel
	case "warn":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}
