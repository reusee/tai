package taivm

import "reflect"

type Pointer struct {
	Target    any
	Key       any
	ArrayType reflect.Type
}
