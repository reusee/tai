package tailang

import (
	"fmt"
	"math"
)

func Plus(a, b any) any {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			return aInt + bInt
		}
	}
	if aFloat, ok := asFloat(a); ok {
		if bFloat, ok := asFloat(b); ok {
			return aFloat + bFloat
		}
	}
	return fmt.Sprint(a) + fmt.Sprint(b)
}

func Minus(a, b any) (any, error) {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			return aInt - bInt, nil
		}
	}
	if aFloat, ok := asFloat(a); ok {
		if bFloat, ok := asFloat(b); ok {
			return aFloat - bFloat, nil
		}
	}
	return nil, fmt.Errorf("invalid operands for -: %v, %v", a, b)
}

func Multiply(a, b any) (any, error) {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			return aInt * bInt, nil
		}
	}
	if aFloat, ok := asFloat(a); ok {
		if bFloat, ok := asFloat(b); ok {
			return aFloat * bFloat, nil
		}
	}
	return nil, fmt.Errorf("invalid operands for *: %v, %v", a, b)
}

func Divide(a, b any) (any, error) {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			if bInt == 0 {
				return nil, fmt.Errorf("integer division by zero")
			}
			return aInt / bInt, nil
		}
	}
	if aFloat, ok := asFloat(a); ok {
		if bFloat, ok := asFloat(b); ok {
			return aFloat / bFloat, nil
		}
	}
	return nil, fmt.Errorf("invalid operands for /: %v, %v", a, b)
}

func Mod(a, b any) (any, error) {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			if bInt == 0 {
				return nil, fmt.Errorf("integer modulo by zero")
			}
			return aInt % bInt, nil
		}
	}
	if aFloat, ok := asFloat(a); ok {
		if bFloat, ok := asFloat(b); ok {
			return math.Mod(aFloat, bFloat), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for %%: %v, %v", a, b)
}

func asInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int8:
		return int(val), true
	case int16:
		return int(val), true
	case int32:
		return int(val), true
	case int64:
		return int(val), true
	case uint:
		return int(val), true
	case uint8:
		return int(val), true
	case uint16:
		return int(val), true
	case uint32:
		return int(val), true
	case uint64:
		return int(val), true
	}
	return 0, false
}

func asFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	}
	if i, ok := asInt(v); ok {
		return float64(i), true
	}
	return 0, false
}
