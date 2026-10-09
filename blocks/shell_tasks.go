package blocks

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TheoryOfShellTasks documents the shell block's task model: the op
// parameter and the session-scoped registry behind it.
const TheoryOfShellTasks = `
A shell block's op parameter selects how its command relates to the
round. run — the default, and the op of a shell block that carries no op
parameter — executes the command and returns its output in the next
round. background starts the command and returns its task number
immediately, so several long independent commands — a full build, a test
suite, a vet run — run concurrently while the model keeps working; the
block protocol charges no round for starting them. output waits for the
named task and returns its status and output. kill terminates the named
task.

A task belongs to the session. The registry is built together with the
shell component, every round of the session shares it, and a task runs
under the run's context, so an abandoned task stops when the run ends. The
model must therefore collect or kill every task it started before the
session ends: after the run no round remains in which to read the output.

op changes when a command's output arrives, never what running the command
does. The two gates — the allowlist and the structural validator — apply
to run and background alike; output and kill address a task the gates
already admitted, so no ungated command ever becomes a task. A collected
task stays collected: a later output block for the same task reads the
same answer instead of blocking, so a retried round that re-emits its
blocks sees the same result.
`

// shellWaitDelay bounds how long a command's Wait waits for output I/O to
// finish after the command exits or its context is cancelled, so a killed
// command returns promptly even when a grandchild still holds the output
// pipes. See TheoryOfShellTasks.
const shellWaitDelay = time.Second

// shellTask is one background shell command. The goroutine writes output
// before it closes done, and readers read output only after <-done, so the
// channel close orders the write and the read; killed is guarded by the
// registry's lock because Kill sets it while the command may still run.
type shellTask struct {
	id      int
	command string
	cancel  context.CancelFunc
	done    chan struct{}
	output  string
	killed  bool
}

// ShellTasks is the session's registry of background shell commands: one
// per session, created with the shell component and shared by every round.
// All methods are safe for concurrent use. See TheoryOfShellTasks.
type ShellTasks struct {
	mu    sync.Mutex
	next  int
	tasks map[int]*shellTask
}

// NewShellTasks creates an empty task registry.
func NewShellTasks() *ShellTasks {
	return &ShellTasks{tasks: make(map[int]*shellTask)}
}

// Start launches command in the background and returns its task number.
// The task runs under ctx, so it stops when the run that started it ends.
// The task's context is released when the command finishes, whether it
// finished on its own or was killed, so a long session does not accumulate
// context registrations. See TheoryOfShellTasks.
func (s *ShellTasks) Start(ctx context.Context, command string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	taskCtx, cancel := context.WithCancel(ctx)
	task := &shellTask{
		id:      s.next,
		command: command,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	s.tasks[task.id] = task
	go func() {
		defer close(task.done)
		defer cancel()
		task.output = executeShellCommand(taskCtx, command)
	}()
	return task.id
}

// Collect waits for the task to finish and returns its output. An unknown
// task reports false, so the caller can name the running tasks in the
// feedback; a task the model killed returns the output it produced before
// it stopped, marked as killed. See TheoryOfShellTasks.
func (s *ShellTasks) Collect(id int) (output string, ok bool) {
	s.mu.Lock()
	task, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok {
		return "", false
	}
	<-task.done
	s.mu.Lock()
	killed := task.killed
	s.mu.Unlock()
	if killed {
		return "Command was killed.\n" + task.output, true
	}
	return task.output, true
}

// Kill terminates the task and reports whether it existed. A task that
// already finished keeps its own output: there is nothing left to stop.
// See TheoryOfShellTasks.
func (s *ShellTasks) Kill(id int) bool {
	s.mu.Lock()
	task, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case <-task.done:
		return true
	default:
	}
	s.mu.Lock()
	task.killed = true
	s.mu.Unlock()
	task.cancel()
	return true
}

// ActiveIDs returns the numbers of the tasks that have not finished, in
// ascending order, so feedback about an unknown task can name what runs.
func (s *ShellTasks) ActiveIDs() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []int
	for id, task := range s.tasks {
		select {
		case <-task.done:
		default:
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

// shellTaskIDsText renders task numbers for feedback about a task that
// does not exist, naming what is running instead of leaving the model to
// guess.
func shellTaskIDsText(ids []int) string {
	if len(ids) == 0 {
		return "no background shell task is running"
	}
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = strconv.Itoa(id)
	}
	return "running background shell tasks: " + strings.Join(names, ", ")
}

// The shell block ops. run is the default; the other three address the
// session's background tasks. See TheoryOfShellTasks.
const (
	shellOpRun        = "run"
	shellOpBackground = "background"
	shellOpOutput     = "output"
	shellOpKill       = "kill"
)

// shellOpOf returns a shell block's op: the block's op attribute, trimmed
// and lowercased, or run when the block carries none. See
// TheoryOfShellTasks.
func shellOpOf(block Block) string {
	op := strings.ToLower(strings.TrimSpace(block.Attributes["op"]))
	if op == "" {
		return shellOpRun
	}
	return op
}

// shellTaskOf returns a shell block's task number: the block's task
// attribute parsed as a positive integer. It reports false when the
// attribute is absent, malformed, or not positive.
func shellTaskOf(block Block) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(block.Attributes["task"]))
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
