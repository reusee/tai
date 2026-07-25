package main

import (
	"cuelang.org/go/cue"
	"github.com/reusee/tai/configs"
)

// NoMemory configs.Config implementation. See flags.TheoryOfConfigFlagParity.

var _ configs.Config = NoMemory(false)

func (n NoMemory) ConfigPaths() []string {
	return []string{"no_memory"}
}

func (n NoMemory) HandleConfig(path string, values []*cue.Value) (any, error) {
	var b bool
	if err := values[0].Decode(&b); err != nil {
		return nil, err
	}
	return NoMemory(b), nil
}
