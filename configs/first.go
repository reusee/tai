package configs

import (
	"errors"
)

func First[T any](loader Loader, path string) T {
	var value T
	if err := loader.AssignFirst(path, &value); err != nil {
		if errors.Is(err, ErrValueNotFound) {
			return value
		}
		panic(err)
	}
	return value
}
