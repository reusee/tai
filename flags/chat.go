package flags

import (
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
