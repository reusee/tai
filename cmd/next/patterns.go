package main

import "github.com/reusee/tai/cmds"

var patterns []string

func init() {
	cmds.Define("-file", cmds.Func(func(pattern string) {
		patterns = append(patterns, pattern)
	}))
}
