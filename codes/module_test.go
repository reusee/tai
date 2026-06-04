package codes

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestModule(t *testing.T) {
	dscope.New(
		new(Module),
		modes.ForTest(t),
	)
}

func TestPatterns(t *testing.T) {
	cmdPatterns["a.go"] = true
	cmdExcludePatterns["b.go"] = true
	defer func() {
		delete(cmdPatterns, "a.go")
		delete(cmdExcludePatterns, "b.go")
	}()

	var m Module
	p := m.Patterns()
	if len(p) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(p))
	}
	if p[0] != "a.go" {
		t.Errorf("expected a.go, got %s", p[0])
	}
	if p[1] != "!b.go" {
		t.Errorf("expected !b.go, got %s", p[1])
	}
	if p[2] != "!LICENSE*" {
		t.Errorf("expected !LICENSE*, got %s", p[2])
	}
}

func TestPatternsLicenseExcluded(t *testing.T) {
	cmdPatterns["LICENSE"] = true
	defer delete(cmdPatterns, "LICENSE")

	var m Module
	p := m.Patterns()
	if len(p) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(p))
	}
	if p[0] != "LICENSE" {
		t.Errorf("expected LICENSE, got %s", p[0])
	}
}