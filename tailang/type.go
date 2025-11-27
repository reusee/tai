package tailang

import "fmt"

func TypeOf(v any) string {
	if v == nil {
		return "nil"
	}
	switch v.(type) {
	case int:
		return "int"
	case float64:
		return "float64"
	case string:
		return "string"
	case bool:
		return "bool"
	case []any:
		return "list"
	case *Block:
		return "block"
	}
	if _, ok := v.(Function); ok {
		return "function"
	}
	return fmt.Sprintf("%T", v)
}
