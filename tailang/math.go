package tailang

import (
	"fmt"
	"math"
)

func Plus(a, b any) any {
	if aInt, ok := a.(int); ok {
		if bInt, ok := b.(int); ok {
			return aInt + bInt
		}
		if bFloat, ok := b.(float64); ok {
			return float64(aInt) + bFloat
		}
	}
	if aFloat, ok := a.(float64); ok {
		if bFloat, ok := b.(float64); ok {
			return aFloat + bFloat
		}
		if bInt, ok := b.(int); ok {
			return aFloat + float64(bInt)
		}
	}
	return fmt.Sprint(a) + fmt.Sprint(b)
}

func Minus(a, b any) (any, error) {
	if aInt, ok := a.(int); ok {
		if bInt, ok := b.(int); ok {
			return aInt - bInt, nil
		}
		if bFloat, ok := b.(float64); ok {
			return float64(aInt) - bFloat, nil
		}
	}
	if aFloat, ok := a.(float64); ok {
		if bFloat, ok := b.(float64); ok {
			return aFloat - bFloat, nil
		}
		if bInt, ok := b.(int); ok {
			return aFloat - float64(bInt), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for -: %v, %v", a, b)
}

func Multiply(a, b any) (any, error) {
	if aInt, ok := a.(int); ok {
		if bInt, ok := b.(int); ok {
			return aInt * bInt, nil
		}
		if bFloat, ok := b.(float64); ok {
			return float64(aInt) * bFloat, nil
		}
	}
	if aFloat, ok := a.(float64); ok {
		if bFloat, ok := b.(float64); ok {
			return aFloat * bFloat, nil
		}
		if bInt, ok := b.(int); ok {
			return aFloat * float64(bInt), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for *: %v, %v", a, b)
}

func Divide(a, b any) (any, error) {
	if aInt, ok := a.(int); ok {
		if bInt, ok := b.(int); ok {
			if bInt == 0 {
				return nil, fmt.Errorf("integer division by zero")
			}
			return aInt / bInt, nil
		}
		if bFloat, ok := b.(float64); ok {
			return float64(aInt) / bFloat, nil
		}
	}
	if aFloat, ok := a.(float64); ok {
		if bFloat, ok := b.(float64); ok {
			return aFloat / bFloat, nil
		}
		if bInt, ok := b.(int); ok {
			return aFloat / float64(bInt), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for /: %v, %v", a, b)
}

func Mod(a, b any) (any, error) {
	if aInt, ok := a.(int); ok {
		if bInt, ok := b.(int); ok {
			if bInt == 0 {
				return nil, fmt.Errorf("integer modulo by zero")
			}
			return aInt % bInt, nil
		}
		if bFloat, ok := b.(float64); ok {
			return math.Mod(float64(aInt), bFloat), nil
		}
	}
	if aFloat, ok := a.(float64); ok {
		if bFloat, ok := b.(float64); ok {
			return math.Mod(aFloat, bFloat), nil
		}
		if bInt, ok := b.(int); ok {
			return math.Mod(aFloat, float64(bInt)), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for %%: %v, %v", a, b)
}
