package taivm

import "reflect"

type Interface struct {
	Name    string
	Methods map[string]reflect.Type
}
