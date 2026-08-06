package records

import (
	"fmt"
	"strconv"

	"github.com/reusee/tai/flags"
)

// SessionID selects a recorded session by database id. Zero means the most
// recent session for the record subcommand.
type SessionID int64

func (Module) SessionID() SessionID {
	return 0
}

var _ flags.Flag = SessionID(0)

func (s SessionID) Keys() map[string]string {
	return map[string]string{
		"-session": "Select a recorded session by id (0 = most recent)",
	}
}

func (s SessionID) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int argument, got empty")
	}
	n, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	ret := SessionID(n)
	return &ret, args[1:], nil
}

// Analyze enables the record subcommand's analysis mode: the selected
// session is fed to the model for improvement analysis.
type Analyze bool

func (Module) Analyze() Analyze {
	return false
}

var _ flags.Flag = Analyze(false)

func (a Analyze) Keys() map[string]string {
	return map[string]string{
		"-analyze": "Analyze a recorded session with the model to seek improvements",
	}
}

func (a Analyze) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	ret := Analyze(true)
	return &ret, args, nil
}

// SessionLimit caps the number of sessions shown by the list mode.
type SessionLimit int

func (Module) SessionLimit() SessionLimit {
	return 10
}

var _ flags.Flag = SessionLimit(0)

func (l SessionLimit) Keys() map[string]string {
	return map[string]string{
		"-limit": "Limit the number of sessions listed by the record subcommand",
	}
}

func (l SessionLimit) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting int argument, got empty")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return nil, nil, err
	}
	ret := SessionLimit(n)
	return &ret, args[1:], nil
}
