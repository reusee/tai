package anytexts

import "github.com/reusee/tai/cmds"

var includeMimeTypes = map[string]bool{}

func init() {
	cmds.Define("-pdf", cmds.Func(func() {
		includeMimeTypes["application/pdf"] = true
	}))
}
