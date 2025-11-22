package codes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reusee/tai/codes/codetypes"
)

type OpenRoot func(dir string) (*os.Root, error)

func (Module) OpenRoot(
	codeProvider codetypes.CodeProvider,
) OpenRoot {
	return func(dir string) (*os.Root, error) {
		rootDirs, err := codeProvider.RootDirs()
		if err != nil {
			return nil, err
		}
		ok := false
		for _, rootDir := range rootDirs {
			rel, err := filepath.Rel(rootDir, dir)
			if err != nil {
				continue
			}
			if !strings.HasPrefix(rel, "..") {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("not in any root dir: %s", dir)
		}

		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}

		return root, nil
	}
}
