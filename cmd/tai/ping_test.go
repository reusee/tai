package main

import (
	"testing"
)

func TestPingCommandRegistered(t *testing.T) {
	cmd := Command{}
	keys := cmd.Keys()
	if _, ok := keys["ping"]; !ok {
		t.Fatal("ping command not registered in Keys()")
	}

	newValue, _, err := cmd.Handle("ping", nil)
	if err != nil {
		t.Fatalf("Handle ping failed: %v", err)
	}
	// Handle returns *Command (a pointer), matching the flags.Flag convention
	// where Handle returns a pointer to a typed value for scope.Fork.
	// See flags.Flag.Handle documentation.
	pingCmd, ok := newValue.(*Command)
	if !ok {
		t.Fatal("Handle ping did not return a *Command")
	}
	if pingCmd.Main == nil {
		t.Fatal("PingCommand has no Main")
	}
}
