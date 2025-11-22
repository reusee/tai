package cmds

func Var[T any](name string) *T {
	var value T

	// set
	Define(name, Func(func(v T) {
		value = v
	}))

	// set zero
	var zero T
	Define(name+".", Func(func() {
		value = zero
	}))

	return &value
}

func Switch(name string) *bool {
	var value bool

	// set true
	Define(name, Func(func() {
		value = true
	}))

	// set false
	Define("!"+name, Func(func() {
		value = false
	}))

	return &value
}

func Collect[T any](name string) *[]T {
	var value []T
	// append
	Define(name, Func(func(v T) {
		value = append(value, v)
	}))
	return &value
}
