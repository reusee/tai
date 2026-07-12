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
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/memlimit"
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
`

func main() {
	cmds.Execute(os.Args[1:])
	memlimit.ApplyFromFlag()
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

		var state generators.State
		state = generators.NewPrompts(
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
		state = generators.NewOutput(state, w, true).WithTools(false)

		phase := buildGenerate(generator, nil)(
			buildChat(generator, nil)(
				nil,
			),
		)
		for phase != nil {
			phase, state, err = phase(ctx, state)
			ce(err)
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
