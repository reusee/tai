package logs

import "testing"

func TestParseCgroupPath(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "cgroup v2 trailing newline",
			content: "0::/user.slice\n",
			want:    "/user.slice",
		},
		{
			name:    "cgroup v1 multiple lines",
			content: "1:cpu,cpuacct:/docker/abc\n2:memory:/docker/abc\n",
			want:    "/docker/abc",
		},
		{
			name:    "cgroup v2 followed by v1",
			content: "0::/user.slice\n1:name=systemd:/other\n",
			want:    "/user.slice",
		},
		{
			name:    "empty input",
			content: "",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCgroupPath(tt.content); got != tt.want {
				t.Fatalf("parseCgroupPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToJournalKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "letters", in: "message", want: "MESSAGE"},
		{name: "leading digit", in: "9lives", want: "_9LIVES"},
		{name: "empty", in: "", want: "_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toJournalKey(tt.in); got != tt.want {
				t.Fatalf("toJournalKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
