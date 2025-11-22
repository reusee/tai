package logs

import (
	"context"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/reusee/tai/cmds"
	slogmulti "github.com/samber/slog-multi"
	slogjournal "github.com/systemd/slog-journal"
)

var level = new(slog.LevelVar)

func init() {
	cmds.Define("-log-debug", cmds.Func(func() {
		level.Set(slog.LevelDebug)
	}).Desc("set log level to debug"))
	cmds.Define("-log-info", cmds.Func(func() {
		level.Set(slog.LevelInfo)
	}).Desc("set log level to info"))
	cmds.Define("-log-warn", cmds.Func(func() {
		level.Set(slog.LevelWarn)
	}).Desc("set log level to warn"))
	cmds.Define("-log-error", cmds.Func(func() {
		level.Set(slog.LevelError)
	}).Desc("set log level to error"))
}

type Logger = *slog.Logger

func (Module) Logger(
	writer Writer,
) Logger {
	var handlers []slog.Handler

	isSystemdService := false
	cgroupPath, err := getCgroupPath()
	if err == nil {
		isSystemdService = strings.HasSuffix(
			path.Dir(cgroupPath),
			".service",
		)
	}

	// local
	var terminalHandler slog.Handler
	if !isSystemdService {
		terminalHandler = slog.NewTextHandler(
			writer,
			&slog.HandlerOptions{
				Level: level,
			},
		)
		handlers = append(handlers, terminalHandler)
	}

	// systemd journal
	journalHandler, err := slogjournal.NewHandler(&slogjournal.Options{
		ReplaceGroup: func(key string) string {
			return toJournalKey(key)
		},
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			a.Key = toJournalKey(a.Key)
			return a
		},
	})
	if err != nil {
		if terminalHandler != nil {
			record := slog.NewRecord(time.Now(), slog.LevelWarn, "new systemd journal handler", 0)
			record.Add("error", err)
			_ = terminalHandler.Handle(context.Background(), record)
		}
	} else {
		handlers = append(handlers, journalHandler)
	}

	return slog.New(&Handler{
		Handler: slogmulti.Fanout(handlers...),
	})
}

func toJournalKey(str string) string {
	str = strings.ToUpper(str)
	str = strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, str)
	return str
}

func getCgroupPath() (string, error) {
	content, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(content), ":")
	if len(parts) >= 3 {
		return parts[2], nil
	}
	return "", nil
}
