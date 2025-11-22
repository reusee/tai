package configs

import "iter"

func All[T any](loader Loader, path string) iter.Seq[T] {
	return func(yield func(T) bool) {
		for value, err := range loader.IterCueValues(path) {
			if err != nil {
				panic(err)
			}
			var v T
			if err := value.Decode(&v); err != nil {
				panic(err)
			}
			if !yield(v) {
				break
			}
		}
	}
}
