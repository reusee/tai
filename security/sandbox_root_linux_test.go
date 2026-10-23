//go:build linux

package security

import (
	"os"
	"testing"
)

func TestIsSandboxReview(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"review selected", []string{"tai", "sandbox"}, true},
		{"review after flags", []string{"tai", "-file", "foo", "sandbox"}, true},
		{"generation session", []string{"tai", "ai"}, false},
		{"no command", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSandboxReview(c.args); got != c.want {
				t.Fatalf("isSandboxReview(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestSetupSandboxRootSkippedForReview(t *testing.T) {
	t.Setenv("CAI_DISABLE_SANDBOX_ROOT", "")
	prev := os.Args
	os.Args = []string{"tai", "sandbox"}
	t.Cleanup(func() { os.Args = prev })
	if err := setupSandboxRoot(t.TempDir()); err != nil {
		t.Fatalf("the review invocation must not need the sandbox root: %v", err)
	}
}
