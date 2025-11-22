package vars

func DerefOrZero[T any](ptr *T) (ret T) {
	if ptr == nil {
		return
	}
	return *ptr
}
