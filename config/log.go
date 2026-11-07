package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Logger is the process logger. SetLevel selects which records are emitted.
var Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &levelVar}))

var levelVar slog.LevelVar

func init() {
	levelVar.Set(slog.LevelInfo)
}

// SetLevel applies logs.level. Unknown names return an error and leave the current level.
func SetLevel(name string) error {
	lv, err := ParseLevel(name)
	if err != nil {
		return err
	}
	levelVar.Set(lv)
	return nil
}

// ParseLevel accepts DEBUG, INFO, WARNING, and ERROR.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "DEBUG":
		return slog.LevelDebug, nil
	case "INFO":
		return slog.LevelInfo, nil
	case "WARNING", "WARN":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logs.level must be DEBUG, INFO, WARNING, or ERROR")
	}
}

// DebugEnabled reports whether voice dumps and the detailed journals should be written.
func DebugEnabled() bool {
	return levelVar.Level() <= slog.LevelDebug
}

func Debug(msg string, args ...any) { Logger.Debug(msg, args...) }
func Info(msg string, args ...any)  { Logger.Info(msg, args...) }
func Warn(msg string, args ...any)  { Logger.Warn(msg, args...) }
func Error(msg string, args ...any) { Logger.Error(msg, args...) }
