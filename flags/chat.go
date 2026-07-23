package flags

import (
	"fmt"

	"github.com/reusee/tai/cmds"
)

type Chats []string

var chats Chats

func init() {
	cmds.Define("chat", cmds.Func(func(content string) {
		chats = append(chats, content)
	}))
}

func (Module) Chats() Chats {
	return chats
}

var _ Flag = Chats(nil)

func (c Chats) Key() string {
	return "chat"
}

func (c Chats) Handle(args []string) (newValue any, remainArgs []string, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("expecting string argument, got empty")
	}
	newValue = append(c, args[0])
	remainArgs = args[1:]
	return
}
