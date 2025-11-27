package tailang

import "fmt"

func Eq(a, b any) bool {
	if a == b {
		return true
	}
	if ai, ok := asInt(a); ok {
		if bi, ok := asInt(b); ok {
			return ai == bi
		}
		if bf, ok := asFloat(b); ok {
			return float64(ai) == bf
		}
	}
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af == bf
		}
		if bi, ok := asInt(b); ok {
			return af == float64(bi)
		}
	}
	return false
}

func Ne(a, b any) bool {
	return !Eq(a, b)
}

func Lt(a, b any) (bool, error) {
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return as < bs, nil
		}
	}
	if ai, ok := asInt(a); ok {
		if bi, ok := asInt(b); ok {
			return ai < bi, nil
		}
		if bf, ok := asFloat(b); ok {
			return float64(ai) < bf, nil
		}
	}
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af < bf, nil
		}
		if bi, ok := asInt(b); ok {
			return af < float64(bi), nil
		}
	}
	return false, fmt.Errorf("invalid operands for <: %v, %v", a, b)
}

func Le(a, b any) (bool, error) {
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return as <= bs, nil
		}
	}
	if ai, ok := asInt(a); ok {
		if bi, ok := asInt(b); ok {
			return ai <= bi, nil
		}
		if bf, ok := asFloat(b); ok {
			return float64(ai) <= bf, nil
		}
	}
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af <= bf, nil
		}
		if bi, ok := asInt(b); ok {
			return af <= float64(bi), nil
		}
	}
	return false, fmt.Errorf("invalid operands for <=: %v, %v", a, b)
}

func Gt(a, b any) (bool, error) {
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return as > bs, nil
		}
	}
	if ai, ok := asInt(a); ok {
		if bi, ok := asInt(b); ok {
			return ai > bi, nil
		}
		if bf, ok := asFloat(b); ok {
			return float64(ai) > bf, nil
		}
	}
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af > bf, nil
		}
		if bi, ok := asInt(b); ok {
			return af > float64(bi), nil
		}
	}
	return false, fmt.Errorf("invalid operands for >: %v, %v", a, b)
}

func Ge(a, b any) (bool, error) {
	if as, ok := a.(string); ok {
		if bs, ok := b.(string); ok {
			return as >= bs, nil
		}
	}
	if ai, ok := asInt(a); ok {
		if bi, ok := asInt(b); ok {
			return ai >= bi, nil
		}
		if bf, ok := asFloat(b); ok {
			return float64(ai) >= bf, nil
		}
	}
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			return af >= bf, nil
		}
		if bi, ok := asInt(b); ok {
			return af >= float64(bi), nil
		}
	}
	return false, fmt.Errorf("invalid operands for >=: %v, %v", a, b)
}
