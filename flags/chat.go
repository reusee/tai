package flags

import (
	"fmt"
	"slices"
)

type Chats []string

func (Module) Chats() (ret Chats) {
	return
}

var _ Flag = Chats(nil)

func (c Chats) Keys() []string {
	return []string{"chat"}
}

func (c Chats) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = append(slices.Clone(c), args[0])
	remainArgs = args[1:]
	return
}
