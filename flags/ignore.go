package flags

import (
	"fmt"
	"maps"
)

type Ignore map[string]bool

func (Module) Ignore() (ret Ignore) {
	return
}

var _ Flag = Ignore(nil)

func (i Ignore) Keys() map[string]string {
	return map[string]string{
		"-ignore":  "Exclude a file or pattern from the context",
		"-skip":    "Alias for -ignore: exclude a file or pattern from the context",
		"-exclude": "Alias for -ignore: exclude a file or pattern from the context",
	}
}

func (i Ignore) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	// Copy the existing map to preserve scope immutability.
	ret := make(Ignore, len(i)+1)
	maps.Copy(ret, i)
	ret[args[0]] = true
	newValue = ret
	remainArgs = args[1:]
	return
}
