package tailang

type NativeFunc func(vm *VM, args []any) (any, error)
