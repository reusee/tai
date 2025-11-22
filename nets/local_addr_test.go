package nets

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

func TestIsLocalAddr(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
		dscope.Provide(configs.NewLoader(nil, "")),
	).Call(func(
		isLocalAddr IsLocalAddr,
	) {
		yes, err := isLocalAddr("127.0.0.1:10000")
		if err != nil {
			t.Fatal(err)
		}
		if !yes {
			t.Fatal()
		}
		yes, err = isLocalAddr("qq.com")
		if err != nil {
			t.Fatal(err)
		}
		if yes {
			t.Fatal()
		}
	})
}
