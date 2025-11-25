package tailang

import (
	"fmt"
	"reflect"
	"strings"
)

type Join struct{}

var _ Function = Join{}

func (j Join) Name() string {
	return "join"
}

func (j Join) Call(sep string, args ...any) (string, error) {
	var parts []string
	for _, arg := range args {
		v := reflect.ValueOf(arg)
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				parts = append(parts, fmt.Sprint(v.Index(i).Interface()))
			}
		} else {
			parts = append(parts, fmt.Sprint(arg))
		}
	}
	return strings.Join(parts, sep), nil
}
