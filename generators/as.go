package generators

func As[T State](state State) (ret T, ok bool) {
	for {
		if state == nil {
			return
		}
		ret, ok = state.(T)
		if ok {
			return
		}
		state = state.Unwrap()
	}
}
