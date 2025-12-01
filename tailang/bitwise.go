package tailang

import (
	"fmt"
	"math/big"
)

func BitAnd(a, b any) (any, error) {
	if aInt, ok := AsInt(a); ok {
		if bInt, ok := AsInt(b); ok {
			return aInt & bInt, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
			return new(big.Int).And(biA, biB), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for &: %v, %v", a, b)
}

func BitNot(a any) (any, error) {
	if aInt, ok := AsInt(a); ok {
		return ^aInt, nil
	}
	if biA, ok := AsBigInt(a); ok {
		return new(big.Int).Not(biA), nil
	}
	return nil, fmt.Errorf("invalid operand for ^ (unary): %v", a)
}

func BitOr(a, b any) (any, error) {
	if aInt, ok := AsInt(a); ok {
		if bInt, ok := AsInt(b); ok {
			return aInt | bInt, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
			return new(big.Int).Or(biA, biB), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for |: %v, %v", a, b)
}

func BitXor(a any, args ...any) (any, error) {
	if len(args) == 0 {
		return BitNot(a)
	}
	if len(args) == 1 {
		b := args[0]
		if aInt, ok := AsInt(a); ok {
			if bInt, ok := AsInt(b); ok {
				return aInt ^ bInt, nil
			}
		}
		if biA, ok := AsBigInt(a); ok {
			if biB, ok := AsBigInt(b); ok {
				return new(big.Int).Xor(biA, biB), nil
			}
		}
		return nil, fmt.Errorf("invalid operands for ^: %v, %v", a, b)
	}
	return nil, fmt.Errorf("invalid number of arguments for ^")
}

func BitClear(a, b any) (any, error) {
	if aInt, ok := AsInt(a); ok {
		if bInt, ok := AsInt(b); ok {
			return aInt &^ bInt, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
			return new(big.Int).AndNot(biA, biB), nil
		}
	}
	return nil, fmt.Errorf("invalid operands for &^: %v, %v", a, b)
}

func LShift(a, b any) (any, error) {
	count, ok := AsInt(b)
	if !ok || count < 0 {
		return nil, fmt.Errorf("invalid shift count: %v", b)
	}

	if aInt, ok := AsInt(a); ok {
		return aInt << count, nil
	}
	if biA, ok := AsBigInt(a); ok {
		return new(big.Int).Lsh(biA, uint(count)), nil
	}
	return nil, fmt.Errorf("invalid operand for <<: %v", a)
}

func RShift(a, b any) (any, error) {
	count, ok := AsInt(b)
	if !ok || count < 0 {
		return nil, fmt.Errorf("invalid shift count: %v", b)
	}

	if aInt, ok := AsInt(a); ok {
		return aInt >> count, nil
	}
	if biA, ok := AsBigInt(a); ok {
		return new(big.Int).Rsh(biA, uint(count)), nil
	}
	return nil, fmt.Errorf("invalid operand for >>: %v", a)
}
