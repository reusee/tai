package main

import (
	"os"

	"github.com/reusee/tai/taigo"
)

func main() {

	var input = os.Stdin
	var name string
	if len(os.Args) > 1 {
		f, err := os.Open(os.Args[1])
		if err != nil {
			os.Stderr.WriteString(err.Error())
			os.Stderr.WriteString("\n")
			os.Exit(-1)
		}
		defer f.Close()
		input = f
		name = os.Args[1]
	}

	vm, err := taigo.NewVM(name, input, nil)
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Stderr.WriteString("\n")
		os.Exit(-1)
	}

	for _, err := range vm.Run {
		if err != nil {
			os.Stderr.WriteString(err.Error())
			os.Stderr.WriteString("\n")
			os.Exit(-1)
		}
	}

}
