package blocks

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/reusee/tai/generators"
)

func TestProcessShellBlocksBackgroundCollect(t *testing.T) {
	// A background command returns its task number immediately; a later
	// output block collects its status and output from the session's
	// registry. See TheoryOfShellTasks.
	tasks := NewShellTasks()
	ctx := context.Background()

	parts, err := ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "background"},
		Body:       "echo background hello",
	}}, ctx, nil, tasks)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	started := string(parts[0].(generators.Text))
	if !strings.Contains(started, "Shell task 1 started") {
		t.Fatalf("background must return the task number, got: %s", started)
	}

	parts, err = ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "output", "task": "1"},
	}}, ctx, nil, tasks)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	collected := string(parts[0].(generators.Text))
	if !strings.Contains(collected, "background hello") {
		t.Fatalf("collecting a task must return its output, got: %s", collected)
	}
	if !strings.Contains(collected, "Command succeeded") {
		t.Fatalf("the collected output must carry the command status, got: %s", collected)
	}
	if !strings.HasSuffix(collected, "\n\n") {
		t.Fatalf("shell output must end with a blank line, got %q", collected)
	}
}

func TestProcessShellBlocksBackgroundRunsConcurrently(t *testing.T) {
	// Two tasks started in one round overlap: collecting both takes about
	// one command's time, not the sum. See TheoryOfShellTasks.
	tasks := NewShellTasks()
	ctx := context.Background()
	start := time.Now()
	if _, err := ProcessShellBlocks([]Block{
		{Kind: "shell", Attributes: map[string]string{"op": "background"}, Body: "sleep 1"},
		{Kind: "shell", Attributes: map[string]string{"op": "background"}, Body: "sleep 1"},
	}, ctx, nil, tasks); err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	for _, id := range []int{1, 2} {
		if _, ok := tasks.Collect(id); !ok {
			t.Fatalf("task %d must exist", id)
		}
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("background tasks must run concurrently, collecting both took %v", elapsed)
	}
}

func TestProcessShellBlocksTaskKillAndErrors(t *testing.T) {
	// kill terminates a task, the op is case-insensitive, and a task or op
	// the session cannot serve is reported with the running tasks named.
	tasks := NewShellTasks()
	ctx := context.Background()
	if _, err := ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "background"},
		Body:       "sleep 3",
	}}, ctx, nil, tasks); err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}

	parts, err := ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "KILL", "task": "1"},
		Body:       "",
	}}, ctx, nil, tasks)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if got := string(parts[0].(generators.Text)); !strings.Contains(got, "Shell task 1 killed") {
		t.Fatalf("kill must confirm the task, got: %s", got)
	}

	parts, err = ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "output", "task": "1"},
	}}, ctx, nil, tasks)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if got := string(parts[0].(generators.Text)); !strings.Contains(got, "Command was killed") {
		t.Fatalf("a killed task must report the kill, got: %s", got)
	}

	parts, err = ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "output", "task": "7"},
		Body:       "",
	}}, ctx, nil, tasks)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if got := string(parts[0].(generators.Text)); !strings.Contains(got, "no shell task 7") {
		t.Fatalf("an unknown task must be reported, got: %s", got)
	}

	parts, err = ProcessShellBlocks([]Block{{
		Kind:       "shell",
		Attributes: map[string]string{"op": "frobnicate"},
		Body:       "echo hello",
	}}, ctx, nil, tasks)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	if got := string(parts[0].(generators.Text)); !strings.Contains(got, "unknown op") {
		t.Fatalf("an unknown op must be rejected, got: %s", got)
	}
}

func TestProcessShellBlocksSpillsLargeOutput(t *testing.T) {
	// A command whose output exceeds the cap reports the spill path, and
	// the full output stays readable. See TheoryOfShellOutputCapture.
	parts, err := ProcessShellBlocks([]Block{{
		Kind: "shell",
		Body: "yes x | head -n 20000",
	}}, context.Background(), nil)
	if err != nil {
		t.Fatalf("ProcessShellBlocks failed: %v", err)
	}
	output := string(parts[0].(generators.Text))
	const marker = "full output at "
	i := strings.Index(output, marker)
	if i < 0 {
		t.Fatalf("large output must name the spill file, got: %s", output[:min(len(output), 400)])
	}
	rest := output[i+len(marker):]
	j := strings.IndexAny(rest, "]\n")
	if j < 0 {
		t.Fatalf("the spill note must close, got: %s", rest[:min(len(rest), 200)])
	}
	path := rest[:j]
	t.Cleanup(func() { os.Remove(path) })
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the spill file must hold the full output: %v", err)
	}
	if len(content) <= maxShellOutputBytes {
		t.Fatalf("the spill file must hold the whole output, got %d bytes", len(content))
	}
	if !strings.HasPrefix(string(content), "x\nx\n") {
		t.Fatal("the spill file must start with the command's first bytes")
	}
}

func TestBoundedCaptureSpillsLargeOutput(t *testing.T) {
	// A stream beyond the cap keeps a head and a tail excerpt and names
	// the spill file, which holds every byte.
	capture := newBoundedCapture(maxShellOutputBytes)
	head := strings.Repeat("H", maxShellOutputBytes)
	over := strings.Repeat("M", shellOutputTailBytes+100)
	tail := strings.Repeat("T", shellOutputTailBytes)
	for _, chunk := range []string{head, over, tail} {
		if _, err := capture.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}
	capture.Close()

	excerpt := capture.String()
	if !strings.HasPrefix(excerpt, "HHHH") {
		t.Fatalf("the excerpt must start with the head, got: %q", excerpt[:16])
	}
	if !strings.Contains(excerpt, "bytes omitted; full output at ") {
		t.Fatalf("the excerpt must name the spill file, got: %s", excerpt)
	}
	if !strings.HasSuffix(excerpt, "TTTT") {
		t.Fatalf("the excerpt must end with the tail, got: %q", excerpt[len(excerpt)-16:])
	}
	t.Cleanup(func() { os.Remove(capture.path) })
	content, err := os.ReadFile(capture.path)
	if err != nil {
		t.Fatalf("the spill file must be readable: %v", err)
	}
	if string(content) != head+over+tail {
		t.Fatalf("the spill file must hold every byte, got %d bytes", len(content))
	}
}

func TestBoundedCaptureKeepsSmallOutputWhole(t *testing.T) {
	// Output within the cap is kept whole and no file is created.
	capture := newBoundedCapture(64)
	if _, err := capture.Write([]byte("hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	capture.Close()
	if capture.spilled {
		t.Fatal("small output must not spill")
	}
	if got := capture.String(); got != "hello" {
		t.Fatalf("small output must stay whole, got %q", got)
	}
}

func TestBoundedCaptureHeadFillsToLimit(t *testing.T) {
	// A write that crosses the cap fills the head excerpt to the cap
	// before spilling, so the in-context excerpt always carries the
	// stream's first maxShellOutputBytes. See TheoryOfShellOutputCapture.
	capture := newBoundedCapture(64)
	if _, err := capture.Write([]byte(strings.Repeat("A", 10))); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if _, err := capture.Write([]byte(strings.Repeat("B", 200))); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	capture.Close()
	t.Cleanup(func() { os.Remove(capture.path) })
	if capture.head.Len() != 64 {
		t.Fatalf("the head excerpt must fill to the cap, got %d bytes", capture.head.Len())
	}
	if !strings.HasPrefix(capture.String(), strings.Repeat("A", 10)+strings.Repeat("B", 54)) {
		t.Fatal("the head excerpt must carry the stream's first bytes")
	}
	content, err := os.ReadFile(capture.path)
	if err != nil {
		t.Fatalf("the spill file must be readable: %v", err)
	}
	if len(content) != 10+200 {
		t.Fatalf("the spill file must hold every byte, got %d", len(content))
	}
}
