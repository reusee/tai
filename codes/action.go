package codes

import (
	"fmt"
	"strings"

	"go/scanner"
	"go/token"

	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/taiconfigs"
	"github.com/reusee/tai/taigo"
	"github.com/reusee/tai/taivm"
	"github.com/reusee/tai/vars"
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

func (Module) Chats(
	loader configs.Loader,
) Chats {
	return configs.First[Chats](loader, "chats")
}

func (Module) ActionArgument(
	loader configs.Loader,
	chats Chats,
	env taiconfigs.ConfigGoEnv,
	flagChats flags.Chats,
) ActionArgument {
	arg := vars.FirstNonZero(
		ActionArgument(strings.Join(flagChats, "\n")),
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

