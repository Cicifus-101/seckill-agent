package logx

import (
	"fmt"
	"log/slog"
	"os"
	"seckill-agent/internal/config"
	"strings"
)

func New(cfg config.LogConfig) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	handlerOptions := &slog.HandlerOptions{
		AddSource: cfg.AddSource,
		Level:     level,
	}

	switch strings.ToLower(cfg.Format) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, handlerOptions)), nil
	case "text", "":
		return slog.New(slog.NewTextHandler(os.Stdout, handlerOptions)), nil
	default:
		return nil, fmt.Errorf("unsupported log format: %s", cfg.Format)
	}
}

func parseLevel(level string) (slog.Leveler, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return nil, fmt.Errorf("unsupported log level: %s", level)
	}
}
