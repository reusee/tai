package main

import (
	"context"
	"io"
	"os"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/vars"
	"golang.org/x/term"
)

var chatArgs = cmds.Var[string]("chat")

func main() {
	cmds.Execute(os.Args[1:])
	ctx := context.Background()

	dscope.New(
		new(Module),
		modes.ForProduction(),
	).Fork(
		dscope.Provide(generators.FallbackModelName("gemini")),
	).Call(func(
		logger logs.Logger,
		getSystemPrompt GetSystemPrompt,
		updateMemoryFunc UpdateMemoryFunc,
		buildGenerate phases.BuildGenerate,
		buildChat phases.BuildChat,
		generator generators.Generator,
	) {

		input := *chatArgs

		stdin := getStdinContent()
		if len(stdin) > 0 {
			input = input + "\n" + string(stdin)
		}
		logger.InfoContext(ctx, "input", "len", len(input))

		systemPrompt, err := getSystemPrompt()
		ce(err)

		var parts []generators.Part

		for _, filePath := range files {
			part, err := filePathToPart(filePath)
			ce(err)
			parts = append(parts, part)
			logger.Info("file",
				"path", filePath,
			)
		}

		parts = append(parts, generators.Text(vars.FirstNonZero(
			input,
		)))

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
		state = generators.NewOutput(state, os.Stdout, true).WithTools(false)
		if !*noMemory {
			state = generators.NewFuncMap(state, updateMemoryFunc)
		}

		phase := buildGenerate(
			generator,
			buildChat(generator, nil),
		)
		for phase != nil {
			phase, state, err = phase(ctx, state)
			ce(err)
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
