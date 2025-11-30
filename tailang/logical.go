package tailang

func Not(v any) bool {
	if b, ok := v.(bool); ok {
		return !b
	}
	return v == nil || v == false
}

func LogicAnd(a, b any) bool {
	return isTruthy(a) && isTruthy(b)
}

func LogicOr(a, b any) bool {
	return isTruthy(a) || isTruthy(b)
}

func isTruthy(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return v != nil && v != false
}
