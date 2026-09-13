package nets

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
)

func TestProxyAddrHandle(t *testing.T) {
	newDef, remainArgs, err := ProxyAddr("").Handle("-proxy", []string{"socks5://127.0.0.1:1080", "rest"})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := newDef.(*ProxyAddr)
	if !ok {
		t.Fatalf("expected *ProxyAddr, got %T", newDef)
	}
	if *def != ProxyAddr("socks5://127.0.0.1:1080") {
		t.Fatalf("unexpected value %q", *def)
	}
	if len(remainArgs) != 1 || remainArgs[0] != "rest" {
		t.Fatalf("unexpected remaining args %v", remainArgs)
	}
}

// TestProxyAddrHandleNoArg reproduces the missing argument guard: -proxy
// with no argument must report an error like every other single-argument
// flag, not panic with an index out of range.
func TestProxyAddrHandleNoArg(t *testing.T) {
	if _, _, err := ProxyAddr("").Handle("-proxy", nil); err == nil {
		t.Fatal("expected error for -proxy with no argument, got nil")
	}

	// The same through flags.Parse: the panic used to escape the parser.
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	)
	if _, err := flags.Parse(scope, []string{"-proxy"}); err == nil {
		t.Fatal("expected error for -proxy with no argument, got nil")
	}
}
