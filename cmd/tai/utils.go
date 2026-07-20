package main

import (
	"io"
	"os"

	"golang.org/x/term"
)

func getStdinContent() (ret []byte) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return nil
	}
	ret, err := io.ReadAll(os.Stdin)
	ce(err)
	return
}
