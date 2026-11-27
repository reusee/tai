package pathutil

import "path/filepath"

// TaiDir is the tai dir of a process: the directory that holds the tree's
// tai state. The command resolves it once and pins the value in the dscope
// scope, and every package that persists per-tree state takes the value
// from the scope instead of resolving a directory of its own. See
// TheoryOfTaiDir.
type TaiDir string

// StateDir returns the .tai directory of the tai dir: the directory that
// holds the self-prompt and the other state files. It is the only way to
// name that directory, so no caller keeps a second, string-based path build
// beside the type. See TheoryOfTaiDir.
func (t TaiDir) StateDir() string {
	return filepath.Join(string(t), StateDirName)
}

// StatePath returns the path of the state file named name inside the tai
// dir's .tai directory. The caller names a constant, never model output, so
// the path stays inside the state directory. See TheoryOfTaiDir.
func (t TaiDir) StatePath(name string) string {
	return filepath.Join(t.StateDir(), name)
}
