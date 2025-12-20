package codes

import (
	"os"

	"github.com/reusee/tai/codes/codetypes"
)

type OpenRoot func(dir string) (*os.Root, error)

func (Module) OpenRoot(
	codeProvider codetypes.CodeProvider,
	patterns Patterns,
) OpenRoot {
	return func(dir string) (*os.Root, error) {
		//TODO check patterns

		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}

		return root, nil
	}
}
