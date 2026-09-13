package logs

import "testing"

// TestIsSystemdServiceCgroup pins the systemd-service detection: the
// canonical layout — the service unit IS the cgroup — must be detected,
// including the trailing newline the raw /proc field carries. The former
// path.Dir-based check never matched that layout, so a service's log
// records were duplicated into both the terminal writer and the journal.
func TestIsSystemdServiceCgroup(t *testing.T) {
	for _, tt := range []struct {
		cgroupPath string
		want       bool
	}{
		// Canonical layout, with the trailing newline of /proc/self/cgroup.
		{"/system.slice/tai.service\n", true},
		{"/system.slice/tai.service", true},
		// A process in a sub-cgroup of the unit.
		{"/system.slice/tai.service/init.scope", true},
		{"/user.slice/user-1000.slice/user@1000.service/app.slice", true},
		// Not a service unit.
		{"/", false},
		{"/user.slice", false},
		{"", false},
		{"\n", false},
	} {
		if got := isSystemdServiceCgroup(tt.cgroupPath); got != tt.want {
			t.Errorf("isSystemdServiceCgroup(%q) = %v, want %v", tt.cgroupPath, got, tt.want)
		}
	}
}

// TestCgroupPathOf pins the per-line parsing of /proc/self/cgroup: the
// file holds one line per mounted hierarchy, so a hybrid layout carries
// v1 controller lines before the unified "0::" line. Splitting the whole
// content on ":" glues the first line's path onto the next hierarchy's
// ID, and the systemd detection never matches on a hybrid layout — a
// service's log records were duplicated into both the terminal writer
// and the journal there, the same symptom the suffix-based detection
// removed for the single-line layout.
func TestCgroupPathOf(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "unified single line",
			content: "0::/system.slice/tai.service\n",
			want:    "/system.slice/tai.service",
		},
		{
			name:    "hybrid prefers the unified line",
			content: "11:blkio:/system.slice/tai.service\n0::/system.slice/tai.service\n",
			want:    "/system.slice/tai.service",
		},
		{
			name:    "v1 layout falls back to the first path",
			content: "11:blkio:/system.slice/tai.service\n10:memory:/system.slice/tai.service\n",
			want:    "/system.slice/tai.service",
		},
		{
			name:    "empty",
			content: "",
			want:    "",
		},
	} {
		if got := cgroupPathOf(tt.content); got != tt.want {
			t.Errorf("%s: cgroupPathOf(%q) = %q, want %q", tt.name, tt.content, got, tt.want)
		}
	}

	// The hybrid layout must let the systemd detection match.
	hybrid := "11:blkio:/system.slice/tai.service\n0::/system.slice/tai.service\n"
	if !isSystemdServiceCgroup(cgroupPathOf(hybrid)) {
		t.Error("hybrid cgroup layout was not detected as a systemd service")
	}
}
