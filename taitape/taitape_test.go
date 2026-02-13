package taitape

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.json")

	writeTape := func(tape *Tape) {
		data, err := json.MarshalIndent(tape, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	readTape := func() *Tape {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var tape Tape
		if err := json.Unmarshal(data, &tape); err != nil {
			t.Fatal(err)
		}
		return &tape
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	vm := &VM{
		FilePath: path,
		Logger:   logger,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("basic advancement", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "step1", Action: "nop"},
				{Name: "step2", Action: "nop"},
			},
		})
		done, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		if done {
			t.Fatal("should not be done")
		}
		tape := readTape()
		if tape.PC != 1 {
			t.Errorf("PC mismatch: expected 1, got %d", tape.PC)
		}
		if tape.Steps[0].Status != "completed" {
			t.Errorf("status mismatch: %s", tape.Steps[0].Status)
		}

		done, err = vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			t.Fatal("should be done")
		}
		tape = readTape()
		if tape.PC != 2 {
			t.Errorf("PC mismatch: %d", tape.PC)
		}
	})

	t.Run("shell and output", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "shell", Action: "shell: echo 'hello world'"},
			},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape()
		if !strings.Contains(tape.Steps[0].Output, "hello world") {
			t.Errorf("output mismatch: %q", tape.Steps[0].Output)
		}
	})

	t.Run("taigo state sync", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "taigo", Action: "taigo: package main; var x = 42; var y = x * 2"},
			},
			Globals: make(map[string]any),
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape()
		if fmt.Sprintf("%v", tape.Globals["x"]) != "42" {
			t.Errorf("global x mismatch: %v", tape.Globals["x"])
		}
		if fmt.Sprintf("%v", tape.Globals["y"]) != "84" {
			t.Errorf("global y mismatch: %v", tape.Globals["y"])
		}
	})

	t.Run("jump by name", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "start", Action: "jump: target"},
				{Name: "skip", Action: "nop"},
				{Name: "target", Action: "nop"},
			},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape()
		if tape.PC != 2 {
			t.Errorf("jump failed: PC is %d", tape.PC)
		}
	})

	t.Run("wait and pause", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "wait", Action: "wait"},
			},
		})
		lastStatus, lastID := "", ""
		done, err := vm.RunStep(ctx, &lastStatus, &lastID)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			t.Fatal("should not be done")
		}
		tape := readTape()
		if tape.PC != 0 || tape.Steps[0].Status != "paused" {
			t.Errorf("wait failed: PC=%d, Status=%s", tape.PC, tape.Steps[0].Status)
		}
		if lastStatus != "paused" {
			t.Errorf("lastStatus mismatch: %s", lastStatus)
		}
	})

	t.Run("exit", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "exit", Action: "exit"},
				{Name: "unreachable", Action: "nop"},
			},
		})
		done, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		if !done {
			t.Fatal("should be done")
		}
		tape := readTape()
		if tape.PC != 2 {
			t.Errorf("PC mismatch: %d", tape.PC)
		}
	})

	t.Run("failed handling", func(t *testing.T) {
		writeTape(&Tape{
			Steps: []*Step{
				{Name: "fail", Action: "shell: exit 1"},
			},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err == nil {
			t.Fatal("expected error from shell exit 1")
		}
		tape := readTape()
		if tape.Steps[0].Status != "failed" {
			t.Errorf("status should be failed, got %s", tape.Steps[0].Status)
		}

		// Subsequent call should detect failed status and not re-execute
		lastStatus, lastID := "", ""
		done, err := vm.RunStep(ctx, &lastStatus, &lastID)
		if err != nil {
			t.Fatal(err)
		}
		if done || lastStatus != "failed" {
			t.Fatal("should stay in failed state")
		}
	})

	t.Run("log pruning", func(t *testing.T) {
		logs := make([]LogEntry, 600)
		writeTape(&Tape{
			Steps: []*Step{{Action: "nop"}},
			Logs:  logs,
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape()
		if len(tape.Logs) > 500 {
			t.Errorf("logs not pruned: %d", len(tape.Logs))
		}
	})
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.json")
	tape := &Tape{
		Steps: []*Step{
			{Name: "s1", Action: "nop"},
			{Name: "s2", Action: "nop"},
		},
	}
	data, _ := json.Marshal(tape)
	os.WriteFile(path, data, 0644)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vm := &VM{
		FilePath: path,
		Logger:   logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := vm.Run(ctx); err != nil {
		t.Fatal(err)
	}

	data, _ = os.ReadFile(path)
	json.Unmarshal(data, tape)
	if tape.PC != 2 {
		t.Errorf("PC mismatch after Run: %d", tape.PC)
	}
}