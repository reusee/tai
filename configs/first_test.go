package configs

import (
	"testing"
)

func TestFirst(t *testing.T) {
	loader := NewLoader([]string{"test.cue"}, testSchema)

	str := First[string](loader, "str")
	if str != "bar" {
		t.Fatalf("got %v", str)
	}

}
