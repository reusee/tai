package blocks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/reusee/tai/generators"
)

const TheoryOfGoTestBlocks = `
Go-test blocks allow the model to run Go tests and receive the output as part
of the next generation round. After making code changes, the model emits a
go-test block to verify correctness. The system runs go test with the specified
arguments and feeds both stdout and stderr back as user content. If tests
fail, the error output is returned so the model can debug and fix the issues
in subsequent rounds. This enables autonomous test-driven development: the
model writes code, runs tests, reads failures, and iterates until all tests
pass.

The go-test block is Go-specific: it only makes sense in Go projects with
a go.mod file. The system prompt instructs the model to use go-test blocks
only when working with Go code. In non-Go projects, the model should rely on
shell blocks for command execution instead.

The block body contains optional arguments passed to go test. If the body is
empty, all tests in the current directory tree (./...) are run. The body is
passed to sh -c as "go test <body>" to handle quoted arguments and shell
expansion correctly.

The go-test block is not a completion signal. The summary and finish blocks
are completion signals for each round (see TheoryOfSummaryCompletionRetry in
codes/generate.go). When the model emits a go-test block, it must still emit
a summary block in the same round to describe what was done, including the
test verification. Without a summary or finish block, the system assumes the
output was truncated and retries the round unnecessarily. This applies to
every round, including debug rounds where tests fail and the go-test
component triggers a new round via Continue. When tests pass, the go-test
component does not trigger a new round; the test output is not fed back to
the model, and other mechanisms (e.g., continue blocks) determine whether
another round follows.

ProcessGoTestBlocks enforces the pass/fail asymmetry at the implementation
level: it only collects output parts when a test run fails, so the model
receives stdout and stderr exclusively when there are failures to debug and
fix. When all tests pass, no parts are returned, the caller has nothing to
append to the state, and no new round is triggered by the go-test component
alone.
`

const GoTestBlockSystemPrompt = `
Go-Test Block Kind:

The "go-test" kind allows you to run Go tests and receive the output as part of the next generation round. After making code changes (especially new or modified test files), emit a go-test block to verify your changes. The system will run go test and feed the results back to you.

**Go-Test Block Format:**

:::<boundary> <go-test>
<optional go test arguments>
:::<boundary> </go-test>

**Rules:**
- Use go-test blocks to verify code changes by running Go tests. Only use go-test blocks in Go projects.
- The body contains optional arguments passed to go test (e.g., -run TestFoo, -v, ./pkg/...). If empty, all tests in the current directory tree (./...) are run.
- Both stdout and stderr are captured. When tests fail, the full output (stdout and stderr) is fed back to you as user content in the next round so you can debug and fix the issues. When tests pass, the output is not returned.
- Prefer running tests after applying change blocks to verify correctness.
- The boundary is a random string chosen by the AI to prevent conflicts with the body content.
- The go-test block is NOT a completion signal. You MUST still emit a summary block in the same round, after the go-test block, describing what was done (including running tests). Every round — including debug rounds where tests fail — must end with a summary block. Without a summary, the system assumes the output was truncated and retries the round unnecessarily.
- The go-test block should appear before the summary block in the response.
`

const GoTestBlockRestatePrompt = `- After making code changes, emit a go-test block to verify:
:::<boundary> <go-test>
<optional go test arguments>
:::<boundary> </go-test>
- If tests fail, the output (stdout and stderr) is fed back for debugging. Fix the issues and try again. If tests pass, the output is not returned.
- Only use go-test blocks in Go projects.
- A go-test block does NOT replace the summary block. You MUST still emit a summary block in the same round, even when emitting a go-test block. Every round must end with a summary.
`

const goTestTimeout = 120 * time.Second

// executeGoTest runs `go test` with the given arguments and returns the
// output and whether the tests failed.
func executeGoTest(ctx context.Context, args string) (string, bool) {
	cmdCtx, cancel := context.WithTimeout(ctx, goTestTimeout)
	defer cancel()

	cmdStr := "go test ./..."
	if strings.TrimSpace(args) != "" {
		cmdStr = "go test " + args
	}

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", cmdStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Sprintf("Go test command: %s\n\nCommand failed with error: %v\nStdout:\n%s\nStderr:\n%s",
			cmdStr, err, stdout.String(), stderr.String()), true
	}
	return fmt.Sprintf("Go test command: %s\n\nCommand succeeded.\nStdout:\n%s\nStderr:\n%s",
		cmdStr, stdout.String(), stderr.String()), false
}

// ProcessGoTestBlocks pops all go-test blocks from parserState, runs the
// tests, and returns the outputs as generator parts alongside a new
// *ParserState with those blocks removed. Output parts are only collected
// when a test run fails, so the model receives stdout and stderr
// exclusively when there are failures to debug and fix. When all tests
// pass, no parts are returned and the caller has nothing to feed back.
// The failed flag indicates whether any test run failed, so callers can
// set Continue to trigger a new round for debugging.
// The original parserState is not modified. Callers must thread the
// returned *ParserState through subsequent block processing and
// reconcile it with the outer state before the next generation round.
// See TheoryOfParserState and TheoryOfGoTestBlocks.
func ProcessGoTestBlocks(parserState *ParserState, ctx context.Context) ([]generators.Part, *ParserState, bool, error) {
	if parserState == nil {
		return nil, nil, false, nil
	}
	goTestBlocks, newParserState := parserState.PopBlocksByKind("go-test")
	if len(goTestBlocks) == 0 {
		return nil, newParserState, false, nil
	}

	var parts []generators.Part
	anyFailed := false
	for _, block := range goTestBlocks {
		args := block.Body
		output, failed := executeGoTest(ctx, args)
		if failed {
			anyFailed = true
			// Only feed test output back to the model when tests fail,
			// so the model can read stdout and stderr to debug and fix
			// the issues. When tests pass, the output is not returned;
			// the caller has nothing to append, and no new round is
			// triggered by the go-test component.
			// See TheoryOfGoTestBlocks.
			parts = append(parts, generators.Text(output))
		}
	}
	return parts, newParserState, anyFailed, nil
}
