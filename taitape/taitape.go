package taitape

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/taigo"
	"github.com/reusee/tai/taipy"
	"github.com/reusee/tai/taivm"
)

type Step struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Action string `json:"action"` // format "type: content"
	Status string `json:"status"` // pending, running, completed, failed, paused
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Step    string    `json:"step"`
	Message string    `json:"message"`
}

type Tape struct {
	PC      int            `json:"pc"`
	Steps   []*Step        `json:"steps"`
	Globals map[string]any `json:"globals"`
	Logs    []LogEntry     `json:"logs"`
}

type VM struct {
	FilePath string
	Logger   logs.Logger
}

func (v *VM) Run(ctx context.Context) error {
	lastStatus := ""
	lastStepID := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		done, err := v.RunStep(ctx, &lastStatus, &lastStepID)
		if err != nil {
			v.Logger.Error("step execution failed", "error", err)
			return err
		}
		if done {
			v.Logger.Info("tape execution completed")
			break
		}
		// Brief pause between steps to prevent CPU pegging in tight loops
		// especially when waiting for human intervention or locks.
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func (v *VM) RunStep(ctx context.Context, lastStatus, lastStepID *string) (bool, error) {
	lockFile := v.FilePath + ".lock"
	if f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL, 0600); err != nil {
		if os.IsExist(err) {
			// Locked by another process or crashed session
			return false, nil
		}
		return false, err
	} else {
		f.Close()
	}
	defer os.Remove(lockFile)

	data, err := os.ReadFile(v.FilePath)
	if err != nil {
		return false, err
	}
	var tape Tape
	if err := json.Unmarshal(data, &tape); err != nil {
		return false, err
	}

	// 1. Check PC bounds
	if tape.PC < 0 || tape.PC >= len(tape.Steps) {
		return true, nil
	}

	step := tape.Steps[tape.PC]

	// 2. Dispatch logic based on status
	switch step.Status {
	case "completed":
		// Already done, advance PC and continue
		tape.PC++
		return v.saveAndContinue(&tape)

	case "paused":
		// Human intervention requested
		if *lastStatus != "paused" || *lastStepID != step.ID {
			v.Logger.Info("step is paused, waiting for intervention", "name", step.Name, "id", step.ID)
			*lastStatus = "paused"
			*lastStepID = step.ID
		}
		return false, nil

	case "failed":
		// Stay here until human resets it
		if *lastStatus != "failed" || *lastStepID != step.ID {
			v.Logger.Info("step is in failed state, waiting for intervention", "name", step.Name, "id", step.ID)
			*lastStatus = "failed"
			*lastStepID = step.ID
		}
		return false, nil

	case "running":
		// Resilience: resume or retry previously interrupted step
		v.Logger.Warn("resuming previously running step", "name", step.Name)

	default:
		// pending or unknown status: proceed to execution
		*lastStatus = ""
		*lastStepID = ""
	}

	// 3. Mark as running and commit to disk (Transition)
	v.Logger.Info("executing step", "name", step.Name, "pc", tape.PC, "action", step.Action)
	step.Status = "running"
	if err := v.saveTape(&tape); err != nil {
		return false, err
	}

	// 4. Execute
	output, nextPC, execErr := v.executeAction(ctx, step, &tape)
	step.Output = output

	if execErr != nil {
		step.Status = "failed"
		step.Error = execErr.Error()
		tape.Logs = append(tape.Logs, LogEntry{
			Time:    time.Now(),
			Step:    step.Name,
			Message: "Error: " + execErr.Error(),
		})
		v.saveTape(&tape)
		*lastStatus = "failed"
		*lastStepID = step.ID
		return false, execErr
	}

	// 5. Finalize (Sync & Commit)
	// Some actions (like wait) might have updated the status themselves
	if step.Status == "running" {
		step.Status = "completed"
	}
	if nextPC != -1 {
		tape.PC = nextPC
	} else if step.Status == "completed" {
		tape.PC++
	}
	tape.Logs = append(tape.Logs, LogEntry{
		Time:    time.Now(),
		Step:    step.Name,
		Message: "Success",
	})

	if step.Status == "paused" || step.Status == "failed" {
		*lastStatus = step.Status
		*lastStepID = step.ID
	}

	return v.saveAndContinue(&tape)
}

func (v *VM) saveAndContinue(tape *Tape) (bool, error) {
	if err := v.saveTape(tape); err != nil {
		return false, err
	}
	return tape.PC >= len(tape.Steps), nil
}

func (v *VM) saveTape(tape *Tape) error {
	// Prune logs to keep tape size manageable
	const maxLogs = 500
	if len(tape.Logs) > maxLogs {
		tape.Logs = tape.Logs[len(tape.Logs)-maxLogs:]
	}

	data, err := json.MarshalIndent(tape, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write
	tmp := v.FilePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, v.FilePath)
}

func (v *VM) executeAction(ctx context.Context, step *Step, tape *Tape) (string, int, error) {
	parts := strings.SplitN(step.Action, ":", 2)
	actionType := strings.TrimSpace(parts[0])
	var content string
	if len(parts) > 1 {
		content = strings.TrimSpace(parts[1])
	}

	switch actionType {
	case "nop":
		return "nop", -1, nil

	case "shell":
		cmd := exec.CommandContext(ctx, "sh", "-c", content)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.String(), -1, err

	case "taigo":
		if tape.Globals == nil {
			tape.Globals = make(map[string]any)
		}
		env := &taigo.Env{
			Globals:    tape.Globals,
			Source:     content,
			SourceName: step.Name,
		}
		vm, err := env.RunVM()
		if err != nil {
			return "", -1, err
		}
		if vm.IsPanicking {
			return "", -1, fmt.Errorf("taigo panic: %v", vm.PanicValue)
		}
		v.syncEnvToTape(vm.Scope, tape)
		return "executed", -1, nil

	case "taipy":
		fn, err := taipy.Compile(step.Name, strings.NewReader(content))
		if err != nil {
			return "", -1, err
		}
		vm := taivm.NewVM(fn)
		if tape.Globals == nil {
			tape.Globals = make(map[string]any)
		}
		for k, val := range tape.Globals {
			vm.Def(k, val)
		}
		var runErr error
		vm.Run(func(i *taivm.Interrupt, err error) bool {
			if err != nil {
				runErr = err
				return false
			}
			select {
			case <-ctx.Done():
				runErr = ctx.Err()
				return false
			default:
			}
			return true
		})
		v.syncEnvToTape(vm.Scope, tape)
		if runErr != nil {
			return "", -1, runErr
		}
		if vm.IsPanicking {
			return "", -1, fmt.Errorf("taipy panic: %v", vm.PanicValue)
		}
		return "executed", -1, nil

	case "jump":
		// content can be index or name/id
		target := strings.TrimSpace(content)
		for i, s := range tape.Steps {
			if s.Name == target || s.ID == target {
				return "jumped to " + target, i, nil
			}
		}
		var idx int
		if _, err := fmt.Sscanf(target, "%d", &idx); err == nil {
			return "jumped to index", idx, nil
		}
		return "", -1, fmt.Errorf("jump target not found: %s", target)

	case "wait":
		step.Status = "paused"
		return "waiting for intervention", -1, nil

	case "exit":
		return "exiting", len(tape.Steps), nil

	default:
		return "", -1, fmt.Errorf("unknown action type: %s", actionType)
	}
}

func (v *VM) syncEnvToTape(env *taivm.Env, tape *Tape) {
	if tape.Globals == nil {
		tape.Globals = make(map[string]any)
	}
	// Sync top-level environment variables back to Tape Globals.
	// This ensures the "Tape as Memory" model where all script side-effects
	// are persisted for the next instruction.
	for _, vVar := range env.Vars {
		// Convention: ignore private/internal variables starting with underscore.
		if strings.HasPrefix(vVar.Name, "_") {
			continue
		}
		// Critical: only persist JSON-serializable values.
		// Native functions, complex pointers, or non-serializable objects
		// are transient and will be lost between steps.
		if _, err := json.Marshal(vVar.Val); err == nil {
			tape.Globals[vVar.Name] = vVar.Val
		}
	}
	// Clean up Globals to ensure the entire map remains serializable.
	for k, val := range tape.Globals {
		if _, err := json.Marshal(val); err != nil {
			delete(tape.Globals, k)
		}
	}
}