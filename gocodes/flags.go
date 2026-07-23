package gocodes

import "github.com/reusee/tai/flags"

type IncludeStdLib bool

func (Module) IncludeStdLib() IncludeStdLib {
	return false
}

var _ flags.Flag = IncludeStdLib(false)

func (i IncludeStdLib) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return IncludeStdLib(true), args, nil
}

func (i IncludeStdLib) Keys() []string {
	return []string{"-include-std"}
}

type ShowTokenCounts bool

func (Module) ShowTokenCounts() ShowTokenCounts {
	return false
}

var _ flags.Flag = ShowTokenCounts(true)

func (s ShowTokenCounts) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	return ShowTokenCounts(true), args, nil
}

func (s ShowTokenCounts) Keys() []string {
	return []string{"-show-token-counts"}
}
