package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taiconfigs"
	"github.com/reusee/tai/vars"
	"golang.org/x/term"
)

const Theory = `
Memory and Tool Usage:
The AI's memory is implemented as a persistent user profile (ai-memory.json).
This profile is fed into the system prompt to provide long-term context.
Updates are handled via the 'update_user_profile' tool, which the AI is instructed
to call whenever it learns something new about the user.

To ensure reliability:
1. Tool calls are strictly separated from user-facing responses in the prompt.
2. While the AI is forbidden from 'simulating' tool calls in text, a fallback mechanism 
   detects and recovers textual pseudo-calls (e.g., update_user_profile(...)) to ensure 
   memory updates even when the model fails to use the structural tool calling mechanism.
   This mechanism is robust against common hallucination patterns, including use of 
   assignment operators (=) instead of colon separators and single quotes in JSON-like lists.
3. Tool visibility is enabled in the output to provide feedback on memory operations, 
   helping to distinguish between a successful structural call and a textual hallucination.
4. Pseudo-call recovery is implemented as a state wrapper that scans assistant text 
   for specific patterns and injects corresponding function call parts into the stream.
5. Fact-based Profiling: To maintain the integrity of long-term memory, the system 
   enforces a "fact-only" policy. The AI is explicitly instructed to avoid 
   speculation, intuition, or unfounded inference, recording only information 
   explicitly expressed by the user or confirmed by objective facts. Crucially, 
   it must distinguish between a user's topical interest (e.g., asking about a 
   medical procedure) and their personal status (e.g., undergoing that procedure). 
   This prevents the user profile from being polluted with hallucinations or 
   unverified assumptions.

Shell and Continue Blocks:
Shell blocks allow the model to execute shell commands and receive the output
as part of the next generation round. This enables autonomous testing, build
verification, and codebase exploration. Shell block execution is disabled by
default for safety; the -shell flag enables it.

Continue blocks allow the model to self-drive multi-turn generation by emitting
a continue block when the task is not yet complete. The system parses the
continue block, extracts its body as the next user message, and automatically
starts a new generation round. This enables the model to produce arbitrarily
long outputs by chaining multiple rounds.

Both block kinds require ParserState in the state chain to intercept and parse
model output incrementally. After each generation cycle, the parser is flushed
and checked for shell and continue blocks. Shell blocks are processed first;
if none are found, continue blocks are processed. If either kind is found, the
results are appended as user content and a new generation cycle begins.
`

func main() {
	cmds.Execute(os.Args[1:])
	ctx := context.Background()

	scope := dscope.New(
		new(Module),
		modes.ForProduction(),
	).Fork(
		new(generators.FallbackModelName("gemini3")),
		new(apps.Name("cmd_ai")),
	)

	scope, err := taiconfigs.TaigoFork(scope)
	ce(err)

	scope.Call(func(
		logger logs.Logger,
		getSystemPrompt GetSystemPrompt,
		currentMemory CurrentMemory,
		appendMemory AppendMemory,
		buildGenerate phases.BuildGenerate,
		buildChat phases.BuildChat,
		generator generators.Generator,
		flagFiles flags.Files,
		flagChats flags.Chats,
	) {

		input := strings.Join(flagChats, "\n")

		stdin := getStdinContent()
		if len(stdin) > 0 {
			input = input + "\n" + string(stdin)
		}
		logger.InfoContext(ctx, "input", "len", len(input))

		systemPrompt, err := getSystemPrompt()
		ce(err)

		var files []string
		for pattern := range flagFiles {
			paths, err := filepath.Glob(pattern)
			if err != nil {
				files = append(files, pattern)
			} else {
				for _, path := range paths {
					info, err := os.Stat(path)
					if err != nil {
						continue
					}
					if info.IsDir() {
						continue
					}
					files = append(files, path)
				}
			}
		}
		sort.Strings(files)

		var parts []generators.Part

		for _, filePath := range files {
			fileParts, err := filePathToParts(filePath)
			ce(err)
			parts = append(parts, fileParts...)
			logger.Info("file",
				"path", filePath,
			)
		}

		// User input is wrapped with markers so the model can distinguish
		// between reference file context and the task request.
		// See TheoryOfContextStructure in files.go.
		parts = append(parts, generators.Text(
			"\n``` begin of user input\n"+vars.FirstNonZero(input)+"\n``` end of user input\n",
		))

		var baseState generators.State
		baseState = generators.NewPrompts(
			systemPrompt,
			[]*generators.Content{
				{
					Role:  "user",
					Parts: parts,
				},
			},
		)
		buf := new(strings.Builder)
		w := io.MultiWriter(os.Stdout, buf)
		baseState = generators.NewOutput(baseState, w, true).WithTools(false)

		// Generation loop with shell and continue block processing.
		// ParserState intercepts model output to extract structured blocks.
		// Shell blocks execute commands and feed results back as user content.
		// Continue blocks feed the block body back as the next user message.
		for {
			parserState := blocks.NewParserState(baseState)
			state := generators.State(parserState)

			phase := buildGenerate(generator, nil)(
				buildChat(generator, nil)(
					nil,
				),
			)
			for phase != nil {
				phase, state, err = phase(ctx, state)
				ce(err)
			}

			// Flush to finalize any unclosed blocks in ParserState.
			state, err = state.Flush()
			ce(err)

			// Update baseState for potential next cycle.
			baseState = parserState.Unwrap()

			// Process shell blocks if enabled.
			if *shellEnabled {
				shellParts, shellErr := blocks.ProcessShellBlocks(parserState)
				if shellErr != nil {
					logger.ErrorContext(ctx, "shell block", "err", shellErr)
				}
				if len(shellParts) > 0 {
					baseState, err = baseState.AppendContent(&generators.Content{
						Role:  "user",
						Parts: shellParts,
					})
					ce(err)
					continue
				}
			}

			// Process continue blocks.
			continueParts := blocks.ProcessContinueBlocks(parserState)
			if len(continueParts) > 0 {
				baseState, err = baseState.AppendContent(&generators.Content{
					Role:  "user",
					Parts: continueParts,
				})
				ce(err)
				continue
			}

			break
		}

		// update memory from block
		if !*noMemory {
			if err := updateMemoryFromBlock(
				currentMemory,
				appendMemory,
				getModelID(generator.Spec()),
				buf.String(),
			); err != nil {
				logger.ErrorContext(ctx, "update memory", "err", err)
			}
		}

	})

}

func getStdinContent() (ret []byte) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}
	ret, err := io.ReadAll(os.Stdin)
	ce(err)
	return
}
