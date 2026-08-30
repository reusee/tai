package main

import (
	"os"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
)

// TestUserPromptNoFilesSkipsDirectoryScan verifies the no-file regime of
// Module.UserPrompt: with UserPromptDirectoryFallback forked to false —
// the value the ai command's Defs install — an empty -file set assembles
// no file context at all. The working directory contains an includable
// file, so a provider call with empty patterns would scan it; the prompt
// must instead be the system prompt restate alone, with no directory
// content, no working directory hint, and no chat bracketing copy (there
// is no context to bracket; the -chat text reaches the model through the
// command's user input marker). See TheoryOfUserPromptFileContext.
func TestUserPromptNoFilesSkipsDirectoryScan(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("test.md", []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return userPromptMockGenerator{}, nil
			}
		},
		func() flags.Chats { return flags.Chats{"hello"} },
		func() UserPromptDirectoryFallback { return false },
	).Call(func(
		userPrompt UserPrompt,
		systemPrompt SystemPrompt,
	) {
		if len(userPrompt) != 1 {
			t.Fatalf("expected only the restate part, got %d parts", len(userPrompt))
		}
		text, ok := userPrompt[0].(generators.Text)
		if !ok {
			t.Fatalf("user prompt part must be a text part, got %T", userPrompt[0])
		}
		if want := components.SystemPromptRestate(string(systemPrompt)); text != want {
			t.Fatal("user prompt must be the system prompt restate alone")
		}
	})
}

// TestAICommandDefsForkNoDirectoryFallback verifies the wiring itself:
// resolving UserPrompt from a scope with AICommand.Defs applied must show
// the ai command's no-file regime, so removing the fork from the command
// Defs fails here even if Module.UserPrompt keeps the capability.
func TestAICommandDefsForkNoDirectoryFallback(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("test.md", []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
	).Fork(
		AICommand.Defs...,
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return userPromptMockGenerator{}, nil
			}
		},
		func() memories.CurrentMemory {
			return func() (*memories.MemoryEntry, error) {
				return new(memories.MemoryEntry), nil
			}
		},
		func() memories.AppendMemory {
			return func(*memories.MemoryEntry) error { return nil }
		},
	).Call(func(
		userPrompt UserPrompt,
		systemPrompt SystemPrompt,
	) {
		if len(userPrompt) != 1 {
			t.Fatalf("expected only the restate part, got %d parts", len(userPrompt))
		}
		text, ok := userPrompt[0].(generators.Text)
		if !ok {
			t.Fatalf("user prompt part must be a text part, got %T", userPrompt[0])
		}
		if want := components.SystemPromptRestate(string(systemPrompt)); text != want {
			t.Fatal("ai command user prompt must be the system prompt restate alone")
		}
	})
}
