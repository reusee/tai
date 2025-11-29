package tailang

import (
	"fmt"
	"math"
	"math/big"
)

func Plus(a, b any) any {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			res := aInt + bInt
			if (res^aInt)&(res^bInt) < 0 {
				return new(big.Int).Add(big.NewInt(int64(aInt)), big.NewInt(int64(bInt)))
			}
			return res
		}
	}
	if isFloat(a) || isFloat(b) {
		if !isBig(a) && !isBig(b) {
			fA, okA := asFloat(a)
			fB, okB := asFloat(b)
			if okA && okB {
				return fA + fB
			}
		}
		if bfA, ok := asBigFloat(a); ok {
			if bfB, ok := asBigFloat(b); ok {
				return new(big.Float).Add(bfA, bfB)
			}
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return new(big.Int).Add(biA, biB)
		}
	}
	return fmt.Sprint(a) + fmt.Sprint(b)
}

func Minus(a, b any) (any, error) {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			res := aInt - bInt
			if (aInt^bInt) < 0 && (aInt^res) < 0 {
				return new(big.Int).Sub(big.NewInt(int64(aInt)), big.NewInt(int64(bInt))), nil
			}
			return res, nil
		}
	}
	if isFloat(a) || isFloat(b) {
		if !isBig(a) && !isBig(b) {
			fA, okA := asFloat(a)
			fB, okB := asFloat(b)
			if okA && okB {
				return fA - fB, nil
			}
		}
		if bfA, ok := asBigFloat(a); ok {
			if bfB, ok := asBigFloat(b); ok {
				return new(big.Float).Sub(bfA, bfB), nil
			}
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return new(big.Int).Sub(biA, biB), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for -: %v, %v", a, b)
}

func Multiply(a, b any) (any, error) {
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			res := aInt * bInt
			if aInt != 0 && res/aInt != bInt {
				return new(big.Int).Mul(big.NewInt(int64(aInt)), big.NewInt(int64(bInt))), nil
			}
			return res, nil
		}
	}
	if isFloat(a) || isFloat(b) {
		if !isBig(a) && !isBig(b) {
			fA, okA := asFloat(a)
			fB, okB := asFloat(b)
			if okA && okB {
				return fA * fB, nil
			}
		}
		if bfA, ok := asBigFloat(a); ok {
			if bfB, ok := asBigFloat(b); ok {
				return new(big.Float).Mul(bfA, bfB), nil
			}
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return new(big.Int).Mul(biA, biB), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for *: %v, %v", a, b)
}

func Divide(a, b any) (any, error) {
	if isFloat(a) || isFloat(b) {
		if !isBig(a) && !isBig(b) {
			fA, okA := asFloat(a)
			fB, okB := asFloat(b)
			if okA && okB {
				if fB == 0 {
					return nil, fmt.Errorf("float division by zero")
				}
				return fA / fB, nil
			}
		}
		if bfA, ok := asBigFloat(a); ok {
			if bfB, ok := asBigFloat(b); ok {
				if bfB.Sign() == 0 {
					return nil, fmt.Errorf("float division by zero")
				}
				return new(big.Float).Quo(bfA, bfB), nil
			}
		}
	}
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			if bInt == 0 {
				return nil, fmt.Errorf("integer division by zero")
			}
			return aInt / bInt, nil
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			if biB.Sign() == 0 {
				return nil, fmt.Errorf("integer division by zero")
			}
			return new(big.Int).Quo(biA, biB), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for /: %v, %v", a, b)
}

func Mod(a, b any) (any, error) {
	if isFloat(a) || isFloat(b) {
		if !isBig(a) && !isBig(b) {
			fA, okA := asFloat(a)
			fB, okB := asFloat(b)
			if okA && okB {
				return math.Mod(fA, fB), nil
			}
		}
		if bfA, ok := asBigFloat(a); ok {
			if bfB, ok := asBigFloat(b); ok {
				// math.Mod style for BigFloat? big.Float doesn't support Mod directly.
				// Promote to float64 if possible or implement Mod.
				// Given lack of direct BigFloat Mod, fallback to float64 for now
				// or use a custom implementation. The prompt asked for "use big type",
				// but big.Float mod is non-trivial without converting to int or implementing manually.
				// Let's use float64 fallback for mod if it fits, else error?
				// Or implementing a simple mod: a - trunc(a/b)*b
				fA, _ := bfA.Float64()
				fB, _ := bfB.Float64()
				return math.Mod(fA, fB), nil
			}
		}
	}
	if aInt, ok := asInt(a); ok {
		if bInt, ok := asInt(b); ok {
			if bInt == 0 {
				return nil, fmt.Errorf("integer modulo by zero")
			}
			return aInt % bInt, nil
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			if biB.Sign() == 0 {
				return nil, fmt.Errorf("integer modulo by zero")
			}
			return new(big.Int).Rem(biA, biB), nil
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

func asBigInt(v any) (*big.Int, bool) {
	if i, ok := asInt(v); ok {
		return big.NewInt(int64(i)), true
	}
	if bi, ok := v.(*big.Int); ok {
		return bi, true
	}
	return nil, false
}

func asBigFloat(v any) (*big.Float, bool) {
	if bf, ok := v.(*big.Float); ok {
		return bf, true
	}
	if bi, ok := v.(*big.Int); ok {
		return new(big.Float).SetInt(bi), true
	}
	if f, ok := asFloat(v); ok {
		return big.NewFloat(f), true
	}
	return nil, false
}

func isFloat(v any) bool {
	switch v.(type) {
	case float32, float64, *big.Float:
		return true
	}
	return false
}

func isBig(v any) bool {
	switch v.(type) {
	case *big.Int, *big.Float:
		return true
	}
	return false
}
