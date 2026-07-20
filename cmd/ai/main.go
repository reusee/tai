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
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/vars"
	"golang.org/x/term"
)

const Theory = `
Memory and Tool Usage:
The AI's memory is a persistent per-model user profile (ai-memory.json) that is
fed into the system prompt for long-term context. The full memory
implementation — profile storage with advisory locking and atomic writes,
memory block parsing, the textual pseudo-call fallback, and the fact-only
profiling policy — lives in the memories package (see memories.TheoryOfMemory).
This command wires memories into the dscope graph, feeds the current profile
into the system prompt, and invokes memories.UpdateMemoryFromBlock after each
generation round to merge newly learned items into the profile.

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

	scope.Call(func(
		logger logs.Logger,
		getSystemPrompt GetSystemPrompt,
		currentMemory memories.CurrentMemory,
		appendMemory memories.AppendMemory,
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

			// Extract the current ParserState from the state chain.
			// With immutable ParserState, the original parserState pointer
			// is not updated by AppendContent; the current *ParserState is
			// the one inside the state returned by the phase chain.
			// See TheoryOfParserState in blocks/parser_state.go.
			finalParserState, ok := generators.As[*blocks.ParserState](state)
			if !ok {
				finalParserState = parserState
			}

			// Flush to finalize any unclosed blocks in ParserState.
			// Flush returns a new *ParserState; use it for subsequent
			// block processing and for extracting the unwrapped base state.
			flushedState, err := finalParserState.Flush()
			ce(err)
			if ps, ok := generators.As[*blocks.ParserState](flushedState); ok {
				finalParserState = ps
			}

			// Update baseState for potential next cycle.
			baseState = finalParserState.Unwrap()

			// Process shell blocks if enabled.
			if *shellEnabled {
				var shellParts []generators.Part
				shellParts, _, shellErr := blocks.ProcessShellBlocks(finalParserState)
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
			var continueParts []generators.Part
			continueParts, finalParserState = blocks.ProcessContinueBlocks(finalParserState)
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
			if err := memories.UpdateMemoryFromBlock(
				currentMemory,
				appendMemory,
				memories.GetModelID(generator.Spec()),
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
