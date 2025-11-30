package tailang

import "fmt"

func Eq(a, b any) bool {
	if a == b {
		return true
	}
	if IsFloat(a) || IsFloat(b) {
		bfA, okA := AsBigFloat(a)
		bfB, okB := AsBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) == 0
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
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
	if IsFloat(a) || IsFloat(b) {
		bfA, okA := AsBigFloat(a)
		bfB, okB := AsBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) < 0, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
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
	if IsFloat(a) || IsFloat(b) {
		bfA, okA := AsBigFloat(a)
		bfB, okB := AsBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) <= 0, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
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
	if IsFloat(a) || IsFloat(b) {
		bfA, okA := AsBigFloat(a)
		bfB, okB := AsBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) > 0, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
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
	if IsFloat(a) || IsFloat(b) {
		bfA, okA := AsBigFloat(a)
		bfB, okB := AsBigFloat(b)
		if okA && okB {
			return bfA.Cmp(bfB) >= 0, nil
		}
	}
	if biA, ok := AsBigInt(a); ok {
		if biB, ok := AsBigInt(b); ok {
			return biA.Cmp(biB) >= 0, nil
		}
	}
	return false, fmt.Errorf("invalid operands for >=: %v, %v", a, b)
}
