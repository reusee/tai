package main

import (
	"os"

	"github.com/reusee/e5"
	"github.com/reusee/tai/tailang"
)

var (
	ce = e5.Check.With(e5.WrapStacktrace)
)

func main() {
	env := tailang.NewEnv()
	tokenizer := tailang.NewTokenizer(os.Stdin)
	_, err := env.Evaluate(tokenizer)
	ce(err)
}
