package codes

import (
	"fmt"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/phases"
	"github.com/reusee/tai/taiconfigs"
	"github.com/reusee/tai/taigo"
	"github.com/reusee/tai/taivm"
	"github.com/reusee/tai/vars"
)

import (
	"go/scanner"
	"go/token"
)

const Theory = `
Dynamic Argument Expansion:
Action arguments (prompts) can contain embedded Go expressions using the \go(expr) syntax.
These expressions are evaluated within the ConfigGoEnv, which includes variables defined
in the hierarchical configuration files (tai.go). If no ConfigGoEnv is provided, a fresh
environment is created to ensure expansion still works for independent expressions.

Parsing of \go(...) blocks uses the standard Go scanner (go/scanner) to correctly handle
nested parentheses and Go literals (like strings containing parentheses) within the expression.
`

type Action interface {
	Name() string
	InitialPhase(cont phases.Phase) phases.Phase
	DefineCmds()
	InitialGenerator() (generators.Generator, error)
}

type ActionArgument string

func expandGoExprs(s string, env *taivm.Env) string {
	cursor := 0
	for {
		startIdx := strings.Index(s[cursor:], `\go(`)
		if startIdx == -1 {
			break
		}
		startIdx += cursor
		exprStart := startIdx + 4

		// Use Go scanner to find matching parenthesis
		var sscanner scanner.Scanner
		fset := token.NewFileSet()
		file := fset.AddFile("", fset.Base(), len(s)-exprStart)
		sscanner.Init(file, []byte(s[exprStart:]), nil, 0)
		depth := 1
		endIdx := -1
		for {
			pos, tok, _ := sscanner.Scan()
			if tok == token.EOF {
				break
			}
			if tok == token.LPAREN {
				depth++
			} else if tok == token.RPAREN {
				depth--
				if depth == 0 {
					endIdx = exprStart + fset.Position(pos).Offset
					break
				}
			}
		}

		if endIdx == -1 {
			cursor = exprStart
			continue
		}

		expr := s[exprStart:endIdx]
		val, err := taigo.Eval[any](env, expr)
		var replacement string
		if err != nil {
			replacement = fmt.Sprintf("[go error: %v]", err)
		} else {
			replacement = fmt.Sprint(val)
		}
		s = s[:startIdx] + replacement + s[endIdx+1:]
		cursor = startIdx + len(replacement)
	}
	return s
}

type Chats map[string]string

var actionNameFlag string

var actionArgumentFlag ActionArgument

func (Module) Chats(
	loader configs.Loader,
) Chats {
	return configs.First[Chats](loader, "chats")
}

func (Module) AllActions(
	chat ActionChat,
	do ActionDo,
	rank ActionRank,
) []Action {
	return []Action{
		chat,
		do,
		rank,
	}
}

func init() {
	scope := dscope.New(
		new(Module),
		modes.ForProduction(),
	)
	scope, err := taiconfigs.TaigoFork(scope)
	if err != nil {
		panic(err)
	}
	scope.Call(func(
		actions []Action,
	) {
		for _, action := range actions {
			action.DefineCmds()
		}
	})
}

func (Module) Action(
	logger logs.Logger,
	loader configs.Loader,
	allActions []Action,
	chat ActionChat,
) (
	action Action,
) {
	defer func() {
		logger.Info("action",
			"name", action.Name(),
			"details", action,
		)
	}()

	name := vars.FirstNonZero(
		actionNameFlag,
		configs.First[string](loader, "action"),
	)
	for _, a := range allActions {
		if a.Name() == name {
			action = a
			break
		}
	}

	if action == nil {
		action = chat
	}

	return
}

func (Module) ActionArgument(
	loader configs.Loader,
	chats Chats,
	env taiconfigs.ConfigGoEnv,
) ActionArgument {
	arg := vars.FirstNonZero(
		actionArgumentFlag,
		configs.First[ActionArgument](loader, "action_argument"),
	)
	if v, ok := chats[string(arg)]; ok {
		arg = ActionArgument(v)
	}

	e := (*taivm.Env)(env)
	if e == nil {
		e = new(taivm.Env)
	}
	arg = ActionArgument(expandGoExprs(string(arg), e))

	return arg
}