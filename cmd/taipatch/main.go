package main

import (
	"fmt"
	"os"

	"github.com/reusee/tai/codes"
)

func main() {
	target := ".AI"
	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	if err := codes.ApplyHunks(target); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied hunks successfully.")
}
