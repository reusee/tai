package logs

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	slogmulti "github.com/samber/slog-multi"
	slogjournal "github.com/systemd/slog-journal"
)

type Logger struct {
	*slog.Logger
}

func (Module) Logger(
	writer Writer,
	level Level,
) Logger {
	var handlers []slog.Handler

	isSystemdService := false
	if cgroupPath, err := getCgroupPath(); err == nil {
		isSystemdService = isSystemdServiceCgroup(cgroupPath)
	}

	// local
	var terminalHandler slog.Handler
	if !isSystemdService {
		terminalHandler = slog.NewTextHandler(
			writer,
			&slog.HandlerOptions{
				Level: level.Level,
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

	return Logger{slog.New(slogmulti.Fanout(handlers...))}
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

// getCgroupPath returns the process's cgroup path from /proc/self/cgroup.
// The parsing is per-line; see cgroupPathOf.
func getCgroupPath() (string, error) {
	content, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	return cgroupPathOf(string(content)), nil
}

// cgroupPathOf extracts the process's cgroup path from /proc/self/cgroup
// content. The file holds one line per mounted hierarchy, so the path is
// taken from one line: splitting the whole content on ":" would glue the
// first line's path onto the next hierarchy's ID, and the systemd
// detection would never match on a hybrid layout. The unified (v2)
// hierarchy line — "0::<path>" — is preferred, because it is where
// systemd's service cgroups live; a v1 layout has no v2 line, so the
// first line carrying a path is used instead.
func cgroupPathOf(content string) string {
	var first string
	for _, line := range strings.Split(content, "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			return parts[2]
		}
		if first == "" {
			first = parts[2]
		}
	}
	return first
}

// isSystemdServiceCgroup reports whether a /proc/self/cgroup hierarchy
// field names a systemd service unit: the unit — a component ending in
// ".service" — is either the cgroup itself (the canonical layout
// "0::/system.slice/tai.service") or an ancestor of it when the process
// sits in a sub-cgroup. The raw field carries a trailing newline, so the
// path is trimmed before matching. Module.Logger suppresses the terminal
// handler under systemd so records are not duplicated into both the
// journal and the service's stderr.
func isSystemdServiceCgroup(cgroupPath string) bool {
	cgroupPath = strings.TrimSpace(cgroupPath)
	return strings.HasSuffix(cgroupPath, ".service") ||
		strings.Contains(cgroupPath, ".service/")
}
