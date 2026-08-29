package blocks

import (
	"strings"
	"testing"
)

func TestSummaryLanguageInstruction(t *testing.T) {
	if got := SummaryLanguageInstruction(""); got != "" {
		t.Fatalf("empty language must return an empty instruction, got %q", got)
	}
	instruction := SummaryLanguageInstruction("zh")
	if !strings.Contains(instruction, "in zh") {
		t.Fatal("instruction must name the configured language")
	}
	if !strings.Contains(instruction, "bullet") {
		t.Fatal("instruction must keep the bullet-list format requirement")
	}
}
