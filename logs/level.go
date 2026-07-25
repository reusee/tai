package logs

import (
	"log/slog"
	"strings"

	"cuelang.org/go/cue"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
)

// Level configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Level{}

type Level struct {
	Level slog.Leveler
}

func (Module) Level() Level {
	return Level{
		Level: slog.LevelInfo,
	}
}

var _ flags.Flag = Level{}

func (l Level) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	switch key {

	case "-log-debug":
		return Level{
			Level: slog.LevelDebug,
		}, args, nil

	case "-log-info":
		return Level{
			Level: slog.LevelInfo,
		}, args, nil

	case "-log-warn":
		return Level{
			Level: slog.LevelWarn,
		}, args, nil

	case "-log-error":
		return Level{
			Level: slog.LevelError,
		}, args, nil

	}

	panic("key not handle: " + key)
}

func (l Level) Keys() map[string]string {
	return map[string]string{
		"-log-debug": "Set log level to debug",
		"-log-info":  "Set log level to info",
		"-log-warn":  "Set log level to warn",
		"-log-error": "Set log level to error",
	}
}

func (l Level) ConfigPaths() []string {
	return []string{"log_level"}
}

func (l Level) HandleConfig(path string, values []*cue.Value) (any, error) {
	var s string
	if err := values[0].Decode(&s); err != nil {
		return nil, err
	}
	return Level{Level: parseLogLevelFromString(s)}, nil
}

// parseLogLevelFromString converts a log level string to a slog.Leveler.
// Unknown strings default to info level.
func parseLogLevelFromString(s string) slog.Leveler {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
