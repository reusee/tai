package gotools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/generators"
)

const TheoryOfGoTestBlocks = `
Go-test blocks allow the model to run Go tests and receive the output as
part of a generation cycle, enabling autonomous test-driven development:
the model writes code, runs tests, reads failures, and iterates until all
tests pass. The go-test block is Go-specific — it only makes sense in Go
projects with a go.mod file — so its prompts and processing live in the
gotools package, not in the language-neutral blocks package; non-Go
projects rely on shell blocks for command execution.
GoTestBlockSystemPrompt is itself the theory text for the body format, the
absolute-path rule, the -run targeting guidance, and the summary
discipline, and none of it is repeated here.

Each non-empty body line is passed as a separate argument to go test via
exec.Command, bypassing the shell entirely, so model-generated content
never reaches a shell interpreter; the empty body runs ./... because the
model does not know the working directory, and the test output includes it
so later blocks can use absolute package paths.

ProcessGoTestBlocks always returns test output, regardless of whether tests
pass or fail; the go-test component feeds it back as user content, always
triggering a new round. Some models run tests first and need the results to
decide whether to continue; withholding output on pass causes the system to
exit prematurely when they intended to proceed after seeing the test
results. By always feeding back stdout and stderr, the model can see pass
results and continue its workflow, or see failure output and debug the
issues.

At the loop level a go-test block never completes a round on its own: a
round with a go-test block but no summary block is retried with feedback
naming the missing summary, and the block is discarded with the failed
attempt and must be re-emitted together with the summary block —
re-emission is what makes the test run happen (see
pipeline.TheoryOfLoops).
`

const GoTestBlockSystemPrompt = `
Go-Test Block Kind:

Use the "go-test" kind to run Go tests and receive the output as part of the next generation round. After making code changes (especially new or modified test files), emit a go-test block to verify the changes. The system will run go test and feed the results back.

**Rules:**
- Use go-test blocks to verify code changes by running Go tests. Only use go-test blocks in Go projects.
- The body contains ONLY the go test arguments, one per line, with no prose. Each non-empty line is passed as a separate argument to the go test command via exec.Command, bypassing the shell to avoid injection. If empty, all tests in the current directory tree (./...) are run.
- **Use absolute paths** for package arguments (e.g., /home/user/project/pkg/...). The current working directory is not known, so relative paths like ./pkg/... are error-prone. The test output includes the working directory so correct absolute paths can be constructed. If the working directory is not yet known, use an empty body to run all tests (./...).
- **Target specific tests**: When modifying or adding a test function, name it in the -run argument so the verification is directly tied to the change. Put -run and the test name on separate lines, followed by the package path. Prefer precise -run patterns over running an entire package. Only fall back to package-level or ./... runs when a broad sanity check is needed or which tests are relevant is not yet known.
- Both stdout and stderr are captured and fed back as user content in the next round, regardless of whether tests pass or fail.
- Prefer running tests after applying change blocks to verify correctness.
- Close the go-test block with its closing line before emitting any other block (e.g., the summary block): the closing line must appear before the next block's opening marker.
- After the go-test block's closing line, emit the summary block IMMEDIATELY, then end the response and wait for the results — never stop at the closing line itself.
- The go-test block is NOT a completion signal. MUST still emit a summary block in the same round, after the go-test block, describing what was done (including running tests). Every round — including debug rounds where tests fail — must end with a summary block.
- Never end a response on a go-test block: after the go-test block's closing line, the next block MUST be the summary block. A response that ends without a summary block is treated as incomplete and retried — its blocks are discarded and must be re-emitted, so the test run is lost unless re-requested with the summary.
- The go-test block should appear before the summary block in the response.
`

const goTestTimeout = 120 * time.Second

// executeGoTest runs `go test` with the given arguments and returns the
// output and whether the tests failed. The working directory is determined
// via os.Getwd and included in the output so the model can construct
// absolute paths for subsequent test runs. The output ends with a blank
// line so consecutive parts in the same round stay paragraph-separated;
// see generators.TheoryOfContentUnitSeparation.
// See TheoryOfGoTestBlocks.
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
		return fmt.Sprintf("Working directory: %s\nGo test command: %s\n\nCommand failed with error: %v\nStdout:\n%s\nStderr:\n%s\n\n",
			workDir, cmdStr, err, stdout.String(), stderr.String()), true
	}
	return fmt.Sprintf("Working directory: %s\nGo test command: %s\n\nCommand succeeded.\nStdout:\n%s\nStderr:\n%s\n\n",
		workDir, cmdStr, stdout.String(), stderr.String()), false
}

// ProcessGoTestBlocks runs Go tests for all go-test blocks and returns the
// outputs as generator parts. Only blocks with Kind "go-test" are processed.
// Test output (stdout and stderr) is always returned, regardless of whether
// tests pass or fail; the go-test component feeds it back as user content,
// always triggering a new round so the model can see the results and
// continue. Withholding output on pass causes some models to exit
// prematurely when they intended to proceed after seeing the test results.
// See TheoryOfGoTestBlocks.
func ProcessGoTestBlocks(bs []blocks.Block, ctx context.Context) ([]generators.Part, error) {
	if len(bs) == 0 {
		return nil, nil
	}
	var parts []generators.Part
	for _, block := range bs {
		if block.Kind != "go-test" {
			continue
		}
		output, _ := executeGoTest(ctx, block.Body)
		parts = append(parts, generators.Text(output))
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return parts, nil
}
