package vars

func PtrTo[T any](v T) *T {
	return &v
}
