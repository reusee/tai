package sandboxfs

import (
	"fmt"
	"os"
	"path/filepath"
)

const TheoryOfDirRecord = `
A record is a place, and the directory layer makes that literal: the record is
one directory of ordinary files instead of one SQL database. Naming the
directory is the whole selection, exactly as naming a database file selects the
SQL layer, so a caller chooses where a session's changes live and nothing else.
The SQL layer's dialect marker has no counterpart here, because a directory
names itself. See TheoryOfDirUpper and TheoryOfUpperLayer.

The record directory's own name and every path beneath it are the record, and
the store refuses to mutate any of them. The SQL layer protects one file and
two siblings; a directory record holds a metadata file per recorded path, a
content file per content-bearing path, and the audit log, so the protected set
is the subtree. A mutation that reached inside would corrupt the record the
review reads, and would enter the record as a change of itself.

The default record of a working directory is a hidden directory beside the
project, and it is preferred over the database file when both exist: a session
that wrote a directory record asked for that layer, and the review command
finds it without the user naming a path. A directory record is opt-in like the
database record — the memory layer remains the default — so a project with
neither file records nothing.
`

// OpenDir opens the sandbox store whose upper layer is the directory record at
// dbDir, over the read-through base directory. The directory and its files are
// created when absent. It admits every write; see OpenDirWithPolicy for a store
// that enforces a write policy. See TheoryOfDirRecord and TheoryOfDirUpper.
func OpenDir(dbDir, base string) (*Store, error) {
	return OpenDirWithPolicy(dbDir, base, nil)
}

// OpenDirWithPolicy opens a sandbox store whose upper layer is the directory
// record at dbDir and whose mutations are admitted by policy: every write,
// directory creation, link, rename, mode change, and deletion asks policy
// about its path, and a non-nil result refuses the operation before the store
// changes. A nil policy admits every write. See TheoryOfWritePolicy and
// TheoryOfDirRecord.
func OpenDirWithPolicy(dbDir, base string, policy func(string) error) (*Store, error) {
	if dbDir == "" {
		return nil, fmt.Errorf("sandboxfs: empty record directory")
	}
	absDir, err := filepath.Abs(dbDir)
	if err != nil {
		return nil, err
	}
	upper, err := openDirUpper(absDir)
	if err != nil {
		return nil, err
	}
	return newStore(base, upper, policy)
}

// DefaultDirPath returns the directory record of a working directory: one
// hidden directory inside it, holding a metadata file per recorded path, a
// content file per content-bearing path, and the audit log. A session on the
// memory layer leaves no such directory. See TheoryOfDirRecord.
func DefaultDirPath(dir string) string {
	return filepath.Join(dir, ".tai-sandbox.d")
}

// DefaultRecordPath returns the record a session of the given working directory
// would leave, for the review command: the directory record when one exists,
// the SQL record otherwise. A directory record is preferred because a session
// that wrote one selected the directory layer, and the review command then
// needs no flag to find it. A project with neither file records nothing, and
// the returned path names the SQL record's default place, whose own absence is
// the command's error to report. See TheoryOfDirRecord and TheoryOfSqlUpper.
func DefaultRecordPath(dir string) string {
	dirRecord := DefaultDirPath(dir)
	if info, err := os.Stat(dirRecord); err == nil && info.IsDir() {
		return dirRecord
	}
	return DefaultDBPath(dir)
}
