package tailang

import "fmt"

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
