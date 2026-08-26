package components

import (
	"strings"
	"testing"
)

func TestDisabledBlocksNotice(t *testing.T) {
	t.Run("empty without kinds", func(t *testing.T) {
		if notice := DisabledBlocksNotice(); notice != "" {
			t.Fatalf("expected empty notice, got %q", notice)
		}
	})

	t.Run("lists kinds with replacement behavior", func(t *testing.T) {
		notice := DisabledBlocksNotice("shell", "continue")
		if !strings.Contains(notice, disabledBlocksNoticeHeader) {
			t.Fatal("notice should carry the header")
		}
		if !strings.Contains(notice, "shell execution is disabled") {
			t.Fatal("notice should describe the shell replacement behavior")
		}
		if !strings.Contains(notice, "continue blocks are not accepted") {
			t.Fatal("notice should describe the continue replacement behavior")
		}
		if strings.Contains(notice, "go-test") {
			t.Fatal("notice should not list kinds that were not requested")
		}
	})

	t.Run("deterministic regardless of input order", func(t *testing.T) {
		a := DisabledBlocksNotice("shell", "continue", "shell", "go-test")
		b := DisabledBlocksNotice("go-test", "continue", "shell")
		if a != b {
			t.Fatalf("notice must be identical for equal kind sets, got:\n%s\nvs:\n%s", a, b)
		}
	})

	t.Run("unknown kinds are skipped", func(t *testing.T) {
		if notice := DisabledBlocksNotice("not-a-kind"); notice != "" {
			t.Fatalf("unknown kinds must be skipped, got %q", notice)
		}
	})
}

func TestDisabledBlocksComponent(t *testing.T) {
	t.Run("inert without kinds", func(t *testing.T) {
		comps := ComponentSet{DisabledBlocksComponent()}
		if len(comps.Processable()) != 0 {
			t.Fatal("a notice component must never be processable")
		}
		if comps.PromptSections() != "" {
			t.Fatal("an empty notice must contribute no prompt section")
		}
		if comps.RestatePrompts() != "" {
			t.Fatal("an empty notice must contribute no restate prompt")
		}
	})

	t.Run("prompt-only with kinds", func(t *testing.T) {
		comps := ComponentSet{DisabledBlocksComponent("shell")}
		if len(comps.Processable()) != 0 {
			t.Fatal("a notice component must never be processable")
		}
		if !strings.Contains(comps.PromptSections(), "shell execution is disabled") {
			t.Fatal("notice component should contribute the disabled-shell prompt section")
		}
	})
}
