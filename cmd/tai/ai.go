package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/memories"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/vars"
)

const TheoryOfAiCommand = `
Memory and Tool Usage:
The AI's memory is a persistent per-model user profile (ai-memory.json) that is
fed into the system prompt for long-term context. The full memory
implementation — profile storage with advisory locking and atomic writes,
memory block parsing, the textual pseudo-call fallback, and the fact-only
profiling policy — lives in the memories package (see memories.TheoryOfMemory).
This command wires memories into the dscope graph, feeds the current profile
into the memory Component's prompt section (assembled into the system prompt
via comps.PromptSections), and invokes memories.UpdateMemoryFromBlock after
each generation round to merge newly learned items into the profile.

The buf Output layer uses showThoughts=false so model reasoning (Thought parts)
is excluded from the buffer used for memory block parsing. Thoughts may contain
illustrative block markers that would interfere with memory block extraction.
The terminal Output (os.Stdout) retains showThoughts=true so the user still
sees reasoning content on screen.

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

All block kinds are wired through the Component mechanism (see
TheoryOfAIComponents), which couples each block kind's system prompt with its
processing function or ProcessingPath. The component list is shared between
AISystemPrompt (prompt assembly) and this generation loop (output processing),
ensuring that any block kind introduced in the prompt always has a matching
processor. Shell and continue blocks are processed in the loop, accumulating
Parts into a single user message for the next round; memory blocks are
processed after the loop by memories.UpdateMemoryFromBlock.
`

func init() {
	cmds.Define("ai", cmds.Func(func() {
		defs = []any{
			modes.ForProduction(),
			new(apps.Name("cmd_ai")),
		}
		mainFunc = func(
			logger logs.Logger,
			getSystemPrompt AISystemPrompt,
			comps AIComponents,
			currentMemory memories.CurrentMemory,
			appendMemory memories.AppendMemory,
			buildGenerate phases.BuildGenerate,
			buildChat phases.BuildChat,
			generator generators.Generator,
			flagFiles flags.Files,
			flagChats flags.Chats,
		) {
			ctx := context.Background()

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

			// Component user prompt parts are appended after file context,
			// before the user's input. See TheoryOfAIComponents and
			// components.TheoryOfComponents.
			parts = append(parts, comps.UserPromptParts()...)

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
			baseState = generators.NewOutput(baseState, os.Stdout, true).WithTools(false)
			// buf captures assistant text for memory block parsing.
			// showThoughts=false excludes Thought parts so model reasoning
			// (which may contain illustrative block markers) does not
			// interfere with memory block extraction.
			// See TheoryOfAiCommand.
			baseState = generators.NewOutput(baseState, buf, false).WithTools(false)

			// Generation loop with block processing via components.
			// The component list couples each block kind's prompt with its
			// processing function, ensuring prompt-processing parity.
			// See TheoryOfAIComponents and components.TheoryOfComponents.
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

				// Process blocks via components. Each component with a Process
				// function is called in registration order. Components that
				// return Parts (e.g., shell, continue) accumulate parts that
				// are appended together after all components are processed.
				// Components that return Continue=true trigger a new round
				// immediately. See components.TheoryOfComponents.
				var combinedParts []generators.Part
				continueRound := false
				currentPs := finalParserState
				for _, comp := range comps.Processable() {
					result := comp.Process(ctx, &components.ProcessContext{
						ParserState: currentPs,
						State:       baseState,
					})
					if result.Err != nil {
						logger.ErrorContext(ctx, "block processing",
							"kind", comp.Kind, "err", result.Err)
					}
					if result.ParserState != nil {
						currentPs = result.ParserState
					}
					combinedParts = append(combinedParts, result.Parts...)
					if result.Continue {
						continueRound = true
						break
					}
				}

				if continueRound || len(combinedParts) > 0 {
					if len(combinedParts) > 0 {
						baseState, err = baseState.AppendContent(&generators.Content{
							Role:  "user",
							Parts: combinedParts,
						})
						ce(err)
					}
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

		}
	}))
}
