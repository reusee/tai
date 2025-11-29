package tailang

import "fmt"

func Eq(a, b any) bool {
	if a == b {
		return true
	}
	if isFloat(a) || isFloat(b) {
		bfA, okA := asBigFloat(a)
		bfB, okB := asBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) == 0
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return biA.Cmp(biB) == 0
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
	if isFloat(a) || isFloat(b) {
		bfA, okA := asBigFloat(a)
		bfB, okB := asBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) < 0, nil
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return biA.Cmp(biB) < 0, nil
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
	if isFloat(a) || isFloat(b) {
		bfA, okA := asBigFloat(a)
		bfB, okB := asBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) <= 0, nil
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return biA.Cmp(biB) <= 0, nil
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
	if isFloat(a) || isFloat(b) {
		bfA, okA := asBigFloat(a)
		bfB, okB := asBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) > 0, nil
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return biA.Cmp(biB) > 0, nil
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
	if isFloat(a) || isFloat(b) {
		bfA, okA := asBigFloat(a)
		bfB, okB := asBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) >= 0, nil
		}
	}
	if biA, ok := asBigInt(a); ok {
		if biB, ok := asBigInt(b); ok {
			return biA.Cmp(biB) >= 0, nil
		}
	}
	return false, fmt.Errorf("invalid operands for >=: %v, %v", a, b)
}
