package taigo

import "github.com/reusee/tai/taivm"

type Package struct {
	Name string
	Init *taivm.Function
}
