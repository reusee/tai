package main

import (
	"os"

	"github.com/reusee/tai/tailang"
)

func main() {
	env := tailang.NewEnv()

	var input = os.Stdin
	if len(os.Args) > 1 {
		f, err := os.Open(os.Args[1])
		if err != nil {
			os.Stderr.WriteString(err.Error())
			os.Stderr.WriteString("\n")
			os.Exit(-1)
		}
		defer f.Close()
		input = f
	}

	tokenizer := tailang.NewTokenizer(input)
	_, err := env.Evaluate(tokenizer)
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Stderr.WriteString("\n")
		os.Exit(-1)
	}
}
