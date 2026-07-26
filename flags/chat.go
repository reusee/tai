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

func (c Chats) Keys() map[string]string {
	return map[string]string{
		"chat": "Add a chat message to the conversation",
	}
}

func (c Chats) Handle(key string, args []string) (newDef any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	ret := append(slices.Clone(c), args[0])
	return &ret, args[1:], nil
}
