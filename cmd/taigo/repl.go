package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chzyer/readline"
	"github.com/reusee/tai/taigo"
	"github.com/reusee/tai/taivm"
)

func runREPL(vm *taivm.VM) {
	var historyFile string
	if home, err := os.UserHomeDir(); err == nil {
		historyFile = filepath.Join(home, ".taigo_history")
	}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:      "> ",
		HistoryFile: historyFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return
	}
	defer rl.Close()
	for {
		line, err := rl.Readline()
		if err != nil { // Ctrl-C or Ctrl-D
			break
		}
		if line == "" {
			continue
		}
		res, err := taigo.Exec(vm, line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else if res != nil {
			fmt.Println(res)
		}
	}
}
