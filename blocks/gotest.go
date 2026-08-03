package blocks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/reusee/tai/generators"
)

const TheoryOfGoTestBlocks = `
Go-test blocks allow the model to run Go tests and receive the output as part
of a generation cycle. After making code changes, the model emits a go-test
block to verify correctness. The system runs go test with the specified
arguments and feeds both stdout and stderr back as user content. If tests
fail, the error output is returned so the model can debug and fix the issues
in subsequent rounds. This enables autonomous test-driven development: the
model writes code, runs tests, reads failures, and iterates until all tests
pass.

The go-test block is Go-specific: it only makes sense in Go projects with
a go.mod file. The system prompt instructs the model to use go-test blocks
only when working with Go code. In non-Go projects, the model should rely on
shell blocks for command execution instead.

The block body contains optional arguments passed to go test, one argument
per line. If the body is empty, all tests in the current directory tree
(./...) are run. Each non-empty line is passed as a separate argument to
go test via exec.Command, bypassing the shell entirely. This avoids shell
injection vulnerabilities that could arise from passing model-generated
content through sh -c.

The model does not know the current working directory, so relative path
arguments (e.g., ./pkg/...) are error-prone: the model may guess the wrong
relative path and test the wrong package or no package at all. The test
output includes the working directory so the model can construct correct
absolute paths (e.g., /home/user/project/pkg/...) for subsequent runs.
When the model does not yet know the working directory, it should use an
empty body to run all tests (./...), which does not require knowing the
directory. After the first test run, the working directory is available in
the output and the model should switch to absolute paths for specific
package tests.

Test commands should target the specific test functions the model modified or
added, rather than running an entire package. Precise -run patterns (e.g.,
-run TestFoo or -run TestBar/subcase) produce faster, more focused feedback
and avoid noise from unrelated test failures. The model should only fall back
to package-level or ./... runs when it needs a broad sanity check or does not
yet know which tests are relevant. After modifying or adding a test function,
the go-test block should name that function in the -run argument so the
verification is directly tied to the change.

The go-test block is not a completion signal. The summary and finish blocks
are completion signals for each round (see TheoryOfSummaryCompletionRetry in
codes/generate.go). When the model emits a go-test block, it must still emit
a summary block in the same round to describe what was done, including the
test verification. Without a summary or finish block, the system assumes the
output was truncated and retries the round unnecessarily. This applies to
every round, including debug rounds where tests fail and the go-test component
produces Parts that trigger a new round. When tests pass, the go-test
component does not produce Parts; the test output is not fed back to the
model, and other mechanisms (e.g., continue blocks) determine whether another
round follows.

ProcessGoTestBlocks enforces the pass/fail asymmetry at the implementation
level: it only collects output parts when a test run fails, so the model
receives stdout and stderr exclusively when there are failures to debug and
fix. When all tests pass, no parts are returned, the caller has nothing to
append to the state, and no new round is triggered by the go-test component
alone.

When tests pass but another component (e.g., continue) triggers a new round,
the go-test component provides BackgroundParts — a pass confirmation message
— that ProcessComponents includes in the combined output alongside the
triggering component's parts. This ensures the model knows the tests passed
and does not re-emit go-test blocks in subsequent rounds, preventing
unnecessary test reruns. BackgroundParts are discarded when no component
triggers a new round, since there is no next round to carry them. This
preserves the pass/fail asymmetry at the function level (ProcessGoTestBlocks
still returns no parts on pass) while ensuring the model is informed of pass
results when they are relevant to the next round.
`

const GoTestBlockSystemPrompt = `
Go-Test Block Kind:

Use the "go-test" kind to run Go tests and receive the output as part of the next generation round. After making code changes (especially new or modified test files), emit a go-test block to verify the changes. The system will run go test and feed the results back.

**Go-Test Block Format (complete example):**

<<齾麐 <go-test>
-run
TestFoo
-v
/absolute/path/to/pkg/...
齾麐

The delimiter 齾麐 in the example is illustrative only: in every block emitted, choose exactly two uncommon Chinese characters as the delimiter, and use the same delimiter on the closing line. The opening marker must start at the beginning of a line, and the closing line is the delimiter alone on its own line. Never write the placeholder text "DELIMITER" or reuse an example delimiter in a real marker.

**Rules:**
- Use go-test blocks to verify code changes by running Go tests. Only use go-test blocks in Go projects.
- The body contains ONLY the go test arguments, one per line, with no prose. Each non-empty line is passed as a separate argument to the go test command via exec.Command, bypassing the shell to avoid injection. If empty, all tests in the current directory tree (./...) are run.
- **Use absolute paths** for package arguments (e.g., /home/user/project/pkg/...). The current working directory is not known, so relative paths like ./pkg/... are error-prone. The test output includes the working directory so correct absolute paths can be constructed. If the working directory is not yet known, use an empty body to run all tests (./...).
- **Target specific tests**: When modifying or adding a test function, name it in the -run argument so the verification is directly tied to the change. Put -run and the test name on separate lines, followed by the package path. Prefer precise -run patterns over running an entire package. Only fall back to package-level or ./... runs when a broad sanity check is needed or which tests are relevant is not yet known.
- Both stdout and stderr are captured. When tests fail, the full output (stdout and stderr) is fed back as user content in the next round for debugging and fixing the issues. When tests pass, the output is not returned.
- Prefer running tests after applying change blocks to verify correctness.
- Close the go-test block with its closing line before emitting any other block (e.g., the summary block): the closing line must appear before the next block's opening marker.
- The go-test block is NOT a completion signal. MUST still emit a summary block in the same round, after the go-test block, describing what was done (including running tests). Every round — including debug rounds where tests fail — must end with a summary block. Without a summary, the system assumes the output was truncated and retries the round unnecessarily.
- The go-test block should appear before the summary block in the response.
`

const GoTestBlockRestatePrompt = `- After making code changes, emit a go-test block to verify:
<<虋灩 <go-test>
<optional go test arguments, one per line>
虋灩
- Each non-empty line in the body is passed as a separate argument to go test via exec.Command, bypassing the shell to avoid injection. If empty, all tests (./...) are run.
- **Use absolute paths** for package arguments (e.g., /home/user/project/pkg/...). The current working directory is not known, so relative paths like ./pkg/... are error-prone. The test output includes the working directory so correct absolute paths can be constructed. If the working directory is not yet known, use an empty body to run all tests (./...).
- **Target specific tests**: When modifying or adding a test function, name it in the -run argument so the verification is directly tied to the change. Put -run and the test name on separate lines, followed by the package path. Prefer precise -run patterns over running an entire package. Only fall back to package-level or ./... runs when a broad sanity check is needed or which tests are relevant is not yet known.
- If tests fail, the output (stdout and stderr) is fed back for debugging. Fix the issues and try again. If tests pass, the output is not returned.
- Only use go-test blocks in Go projects.
- A go-test block does NOT replace the summary block. MUST still emit a summary block in the same round, even when emitting a go-test block. Every round must end with a summary.
- The example delimiter 虋灩 is illustrative: choose two uncommon Chinese characters as the delimiter, the SAME delimiter on the closing line. The opening marker starts at the beginning of a line; the closing line is the delimiter alone. Never write the placeholder text "DELIMITER" or reuse an example delimiter literally.`

const goTestTimeout = 120 * time.Second

// executeGoTest runs `go test` with the given arguments and returns the
// output and whether the tests failed. The working directory is determined
// via os.Getwd and included in the output so the model can construct
// absolute paths for subsequent test runs. See TheoryOfGoTestBlocks.
//
// Arguments are parsed from a newline-separated list: each non-empty line
// becomes a separate argument passed directly to the go binary via
// exec.Command, bypassing the shell entirely to avoid injection.
func executeGoTest(ctx context.Context, args string) (string, bool) {
	cmdCtx, cancel := context.WithTimeout(ctx, goTestTimeout)
	defer cancel()

	// Parse arguments from a newline-separated list. Each non-empty
	// line becomes a separate argument passed directly to go test via
	// exec.Command, bypassing the shell entirely to avoid injection.
	// See TheoryOfGoTestBlocks.
	var testArgs []string
	for line := range strings.SplitSeq(args, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			testArgs = append(testArgs, line)
		}
	}
	if len(testArgs) == 0 {
		testArgs = []string{"./..."}
	}

	// Determine the working directory so it can be included in the output.
	// The model does not know the current directory, so relative paths in
	// test commands are error-prone. Including the working directory lets
	// the model construct absolute paths for subsequent test runs.
	// See TheoryOfGoTestBlocks.
	workDir, dirErr := os.Getwd()
	if dirErr != nil {
		workDir = "(unknown)"
	}

	fullArgs := make([]string, 0, len(testArgs)+1)
	fullArgs = append(fullArgs, "test")
	fullArgs = append(fullArgs, testArgs...)
	cmd := exec.CommandContext(cmdCtx, "go", fullArgs...)
	if dirErr == nil {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	cmdStr := "go test " + strings.Join(testArgs, " ")
	if err != nil {
		return fmt.Sprintf("Working directory: %s\nGo test command: %s\n\nCommand failed with error: %v\nStdout:\n%s\nStderr:\n%s",
			workDir, cmdStr, err, stdout.String(), stderr.String()), true
	}
	return fmt.Sprintf("Working directory: %s\nGo test command: %s\n\nCommand succeeded.\nStdout:\n%s\nStderr:\n%s",
		workDir, cmdStr, stdout.String(), stderr.String()), false
}

// ProcessGoTestBlocks runs Go tests for all go-test blocks and returns the
// outputs as generator parts. Only blocks with Kind "go-test" are processed.
// Output parts are only collected when a test run fails, so the model
// receives stdout and stderr exclusively when there are failures to debug
// and fix. When all tests pass, no parts are returned and the caller has
// nothing to feed back. The failed flag indicates whether any test run
// failed, so callers can set Continue to trigger a new round for debugging.
// See TheoryOfGoTestBlocks.
func ProcessGoTestBlocks(blocks []Block, ctx context.Context) ([]generators.Part, bool, error) {
	if len(blocks) == 0 {
		return nil, false, nil
	}
	var parts []generators.Part
	anyFailed := false
	for _, block := range blocks {
		if block.Kind != "go-test" {
			continue
		}
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
	return parts, anyFailed, nil
}
