package logs

import (
	"log/slog"

	"github.com/reusee/tai/flags"
)

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
