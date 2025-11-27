package main

import (
	"os"

	"github.com/reusee/tai/tailang"
)

func main() {
	env := tailang.NewEnv()
	tokenizer := tailang.NewTokenizer(os.Stdin)
	_, err := env.Evaluate(tokenizer)
	if err != nil {
		os.Stderr.WriteString(err.Error())
		os.Stderr.WriteString("\n")
		os.Exit(-1)
	}
}
