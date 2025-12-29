package main

import (
	"io"
	"os"
	"strings"

	"github.com/reusee/tai/taigo"
)

func main() {
	var replMode bool
	var fileName string
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-r" {
			replMode = true
		} else if fileName == "" {
			fileName = os.Args[i]
		}
	}

	var input io.Reader
	var name string
	if fileName != "" {
		f, err := os.Open(fileName)
		if err != nil {
			os.Stderr.WriteString(err.Error())
			os.Stderr.WriteString("\n")
			os.Exit(-1)
		}
		defer f.Close()
		input = f
		name = fileName
	} else if replMode {
		input = strings.NewReader("package main")
		name = "repl"
	} else {
		input = os.Stdin
	}

	vm, err := taigo.NewVM(name, input, nil)
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Stderr.WriteString("\n")
		os.Exit(-1)
	}

	if name != "repl" {
		for _, err := range vm.Run {
			if err != nil {
				os.Stderr.WriteString(err.Error())
				os.Stderr.WriteString("\n")
				os.Exit(-1)
			}
		}
	}

	if replMode {
		runREPL(vm)
	}
}
