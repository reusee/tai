package cmds

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

func (p *Executor) PrintUsage() {
	fmt.Println("Available commands:")
	p.printCommandsUsage(p.commands, 0)
}

func (p *Executor) printCommandsUsage(commands map[string]*Command, indent int) {
	names := slices.Sorted(maps.Keys(commands))

	printed := make(map[*Command]bool)
	for _, name := range names {
		cmd := commands[name]
		if printed[cmd] {
			continue
		}
		printed[cmd] = true

		indentStr := strings.Repeat("    ", indent)
		fmt.Printf("%s%s", indentStr, name)
		for _, name := range cmd.Aliases {
			fmt.Printf(" %s", name)
		}

		if cmd.Func.IsValid() {
			funcType := cmd.Func.Type()
			for i := range funcType.NumIn() {
				argType := funcType.In(i)
				fmt.Printf(" <%s>", argType.Kind().String())
			}
		}

		if cmd.Description != "" {
			fmt.Printf(" : %s", cmd.Description)
		}
		fmt.Println()

		if len(cmd.Subs) > 0 {
			p.printCommandsUsage(cmd.Subs, indent+1)
		}
	}
}
