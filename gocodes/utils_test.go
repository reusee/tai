package gocodes

import (
	"path/filepath"
	"runtime"
)

var testdataDir = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(
		filepath.Dir(file),
		"..",
		"testdata",
	)
}()
