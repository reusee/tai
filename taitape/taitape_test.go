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

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	vm := &VM{
		FilePath: path,
		Logger:   logger,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("basic advancement", func(t *testing.T) {
		writeTape(t, path, &Tape{
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
		tape := readTape(t, path)
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
		tape = readTape(t, path)
		if tape.PC != 2 {
			t.Errorf("PC mismatch: %d", tape.PC)
		}
	})

	t.Run("shell and output", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{
				{Name: "shell", Action: "shell: echo 'hello world'"},
			},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
		if !strings.Contains(tape.Steps[0].Output, "hello world") {
			t.Errorf("output mismatch: %q", tape.Steps[0].Output)
		}
	})

	t.Run("taigo state sync", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{
				{Name: "taigo", Action: "taigo: package main; var x = 42; var y = x * 2"},
			},
			Globals: make(map[string]any),
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
		if fmt.Sprintf("%v", tape.Globals["x"]) != "42" {
			t.Errorf("global x mismatch: %v", tape.Globals["x"])
		}
		if fmt.Sprintf("%v", tape.Globals["y"]) != "84" {
			t.Errorf("global y mismatch: %v", tape.Globals["y"])
		}
	})

	t.Run("taipy action", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{
				{Name: "py", Action: "taipy: a = 10; b = a + 5"},
			},
			Globals: make(map[string]any),
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
		if fmt.Sprintf("%v", tape.Globals["a"]) != "10" {
			t.Errorf("py global a mismatch: %v", tape.Globals["a"])
		}
		if fmt.Sprintf("%v", tape.Globals["b"]) != "15" {
			t.Errorf("py global b mismatch: %v", tape.Globals["b"])
		}
	})

	t.Run("jump by name", func(t *testing.T) {
		writeTape(t, path, &Tape{
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
		tape := readTape(t, path)
		if tape.PC != 2 {
			t.Errorf("jump by name failed: PC is %d", tape.PC)
		}
	})

	t.Run("jump by index", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{
				{Name: "start", Action: "jump: 2"},
				{Name: "skip", Action: "nop"},
				{Name: "target", Action: "nop"},
			},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
		if tape.PC != 2 {
			t.Errorf("jump by index failed: PC is %d", tape.PC)
		}
	})

	t.Run("wait and pause", func(t *testing.T) {
		writeTape(t, path, &Tape{
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
		tape := readTape(t, path)
		if tape.PC != 0 || tape.Steps[0].Status != "paused" {
			t.Errorf("wait failed: PC=%d, Status=%s", tape.PC, tape.Steps[0].Status)
		}
		if lastStatus != "paused" {
			t.Errorf("lastStatus mismatch: %s", lastStatus)
		}
	})

	t.Run("exit", func(t *testing.T) {
		writeTape(t, path, &Tape{
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
		tape := readTape(t, path)
		if tape.PC != 2 {
			t.Errorf("PC mismatch: %d", tape.PC)
		}
	})

	t.Run("failed handling", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{
				{Name: "fail", Action: "shell: exit 1"},
			},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err == nil {
			t.Fatal("expected error from shell exit 1")
		}
		tape := readTape(t, path)
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

	t.Run("resilience: resume running step", func(t *testing.T) {
		writeTape(t, path, &Tape{
			PC: 0,
			Steps: []*Step{
				{Name: "running_step", Action: "shell: echo 'resumed'", Status: "running"},
			},
		})
		// RunStep should notice "running" and execute it anyway
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
		if tape.Steps[0].Status != "completed" {
			t.Errorf("should have completed, status: %s", tape.Steps[0].Status)
		}
		if !strings.Contains(tape.Steps[0].Output, "resumed") {
			t.Errorf("output missing: %q", tape.Steps[0].Output)
		}
	})

	t.Run("sync filtering non-serializable", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{
				{Name: "sync", Action: "taigo: package main; var x = 123; func f() {}"},
			},
			Globals: make(map[string]any),
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
		if fmt.Sprintf("%v", tape.Globals["x"]) != "123" {
			t.Errorf("global x missing: %v", tape.Globals["x"])
		}
		// 'f' is a function (non-serializable in this context)
		if _, ok := tape.Globals["f"]; ok {
			t.Error("function f should have been filtered out of globals")
		}
	})

	t.Run("log pruning", func(t *testing.T) {
		logs := make([]LogEntry, 600)
		writeTape(t, path, &Tape{
			Steps: []*Step{{Action: "nop"}},
			Logs:  logs,
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err != nil {
			t.Fatal(err)
		}
		tape := readTape(t, path)
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

func writeTape(t *testing.T, path string, tape *Tape) {
	data, err := json.MarshalIndent(tape, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func readTape(t *testing.T, path string) *Tape {
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

func TestLocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.json")
	writeTape(t, path, &Tape{Steps: []*Step{{Action: "nop"}}})

	// Create lock file
	lockFile := path + ".lock"
	if err := os.WriteFile(lockFile, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	vm := &VM{FilePath: path, Logger: slog.Default()}
	done, err := vm.RunStep(context.Background(), new(string), new(string))
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Error("should not be done (locked)")
	}

	// Remove lock and try again
	if err := os.Remove(lockFile); err != nil {
		t.Fatal(err)
	}
	done, err = vm.RunStep(context.Background(), new(string), new(string))
	if err != nil {
		t.Fatal(err)
	}
	// PC should have advanced
	tape := readTape(t, path)
	if tape.PC != 1 {
		t.Errorf("PC mismatch: %d", tape.PC)
	}
}

func TestInterLanguageState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.json")
	writeTape(t, path, &Tape{
		Steps: []*Step{
			{Name: "step1", Action: "taigo: package main; var count = 10"},
			{Name: "step2", Action: "taipy: count += 5"},
			{Name: "step3", Action: "taigo: package main; var final = count * 2"},
		},
		Globals: make(map[string]any),
	})

	vm := &VM{FilePath: path, Logger: slog.Default()}
	ctx := context.Background()

	// Step 1: count = 10
	if _, err := vm.RunStep(ctx, new(string), new(string)); err != nil {
		t.Fatal(err)
	}
	// Step 2: count = 15
	if _, err := vm.RunStep(ctx, new(string), new(string)); err != nil {
		t.Fatal(err)
	}
	// Step 3: final = 30
	if _, err := vm.RunStep(ctx, new(string), new(string)); err != nil {
		t.Fatal(err)
	}

	tape := readTape(t, path)
	if fmt.Sprintf("%v", tape.Globals["count"]) != "15" {
		t.Errorf("count mismatch: %v", tape.Globals["count"])
	}
	if fmt.Sprintf("%v", tape.Globals["final"]) != "30" {
		t.Errorf("final mismatch: %v", tape.Globals["final"])
	}
}

func TestActionErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.json")
	vm := &VM{FilePath: path, Logger: slog.Default()}
	ctx := context.Background()

	t.Run("taigo error", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{{Name: "err", Action: "taigo: package main; syntax error"}},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err == nil {
			t.Fatal("expected error")
		}
		tape := readTape(t, path)
		if tape.Steps[0].Status != "failed" {
			t.Errorf("status mismatch: %s", tape.Steps[0].Status)
		}
	})

	t.Run("taipy error", func(t *testing.T) {
		writeTape(t, path, &Tape{
			Steps: []*Step{{Name: "err", Action: "taipy: 1 / 0"}},
		})
		_, err := vm.RunStep(ctx, new(string), new(string))
		if err == nil {
			t.Fatal("expected error")
		}
		tape := readTape(t, path)
		if tape.Steps[0].Status != "failed" {
			t.Errorf("status mismatch: %s", tape.Steps[0].Status)
		}
	})
}

func TestEmptyTape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tape.json")
	if err := os.WriteFile(path, []byte(`{"pc": 0, "steps": []}`), 0644); err != nil {
		t.Fatal(err)
	}
	vm := &VM{FilePath: path, Logger: slog.Default()}
	done, err := vm.RunStep(context.Background(), new(string), new(string))
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Error("empty tape should be done immediately")
	}
}