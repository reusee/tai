package flags

import (
	"errors"
	"strings"
	"testing"

	"github.com/reusee/dscope"
)

func TestParseHelp(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-help"})
	var helpErr *HelpError
	if !errors.As(err, &helpErr) {
		t.Fatalf("expected HelpError, got: %v", err)
	}
	if !strings.Contains(helpErr.Usage, "chat") {
		t.Fatal("help usage should contain flag descriptions")
	}
}

func TestParseHelpLongForm(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"--help"})
	var helpErr *HelpError
	if !errors.As(err, &helpErr) {
		t.Fatalf("expected HelpError, got: %v", err)
	}
}

func TestParseHelpShortForm(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-h"})
	var helpErr *HelpError
	if !errors.As(err, &helpErr) {
		t.Fatalf("expected HelpError, got: %v", err)
	}
}

func TestParseUnknownFlagShowsUsage(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "Available flags:") {
		t.Fatalf("error should include usage, got: %v", err)
	}
}

func TestParseDuplicateKey(t *testing.T) {
	scope := dscope.New(Module{}, dupKeyModule{})
	_, err := Parse(scope, nil)
	if err == nil {
		t.Fatal("expected error for duplicate key registration")
	}
	if !strings.Contains(err.Error(), "duplicate flag key") {
		t.Fatalf("expected duplicate key error, got: %v", err)
	}
}

type dupKeyModule struct {
	dscope.Module
}

type DupFlag string

var _ Flag = DupFlag("")

func (DupFlag) Keys() map[string]string {
	return map[string]string{"chat": "duplicate chat flag"}
}

func (DupFlag) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return DupFlag("dup"), args, nil
}

func (dupKeyModule) DupFlag() DupFlag {
	return DupFlag("")
}

func TestUsage(t *testing.T) {
	scope := dscope.New(Module{})
	usage := Usage(scope)
	if !strings.Contains(usage, "chat") {
		t.Fatal("usage should contain 'chat' flag")
	}
	if !strings.Contains(usage, "Add a chat message") {
		t.Fatal("usage should contain chat flag description")
	}
}

func TestFormatUsage(t *testing.T) {
	descriptions := map[string]string{
		"zflag": "Z description",
		"aflag": "A description",
	}
	usage := FormatUsage(descriptions)
	if !strings.Contains(usage, "Available flags:") {
		t.Fatal("usage should contain 'Available flags:' header")
	}
	if !strings.Contains(usage, "aflag") || !strings.Contains(usage, "zflag") {
		t.Fatal("usage should contain all flags")
	}
	// Flags should be sorted alphabetically
	aflagIdx := strings.Index(usage, "aflag")
	zflagIdx := strings.Index(usage, "zflag")
	if aflagIdx == -1 || zflagIdx == -1 || aflagIdx > zflagIdx {
		t.Fatalf("flags should be sorted alphabetically, got: %s", usage)
	}
}
