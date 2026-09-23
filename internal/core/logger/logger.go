package logger

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Agentropism/vtuber-agent-go/internal/core/config"
)

// 日志走标准库 slog：不再需要每个包各自持有 logger、再靠 SetLogger 注入——
// ProvideLogger 里 slog.SetDefault 设一次，全局生效（由 app.Initialize 调用）。
func ProvideLogger(cfg *config.Config) (*slog.Logger, error) {
	handler := &ringHandler{
		inner: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
			Level:     parseLevel(cfg.Log.Level),
			AddSource: true,
		}),
		ring: defaultRing,
	}

	log := slog.New(handler)
	slog.SetDefault(log)

	return log, nil
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// 下面是给本项目的薄封装：文案大量是 printf 风格的中文格式串，而 slog 只有结构化形态，
// 直接调会在上百处散落 fmt.Sprintf。文案保持不变（人工排查与文档里的判据都在 grep 它们）。
func Debugf(format string, args ...any) { slog.Debug(fmt.Sprintf(format, args...)) }
func Infof(format string, args ...any)  { slog.Info(fmt.Sprintf(format, args...)) }
func Warnf(format string, args ...any)  { slog.Warn(fmt.Sprintf(format, args...)) }
func Errorf(format string, args ...any) { slog.Error(fmt.Sprintf(format, args...)) }

// 结构化形态（少数调用点用的是「消息 + 键值对」）。
func Debug(msg string, args ...any) { slog.Debug(msg, args...) }
func Info(msg string, args ...any)  { slog.Info(msg, args...) }
func Warn(msg string, args ...any)  { slog.Warn(msg, args...) }
func Error(msg string, args ...any) { slog.Error(msg, args...) }
