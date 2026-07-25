package logs

import (
	"log/slog"
	"strings"

	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// Level configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = Level{}

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
