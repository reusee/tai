package sandboxfs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
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

The directory record is the default the tai tool selects: one directory per
session, under the working directory's ".tai/sandboxfs", named
"<time>.<random>" (SessionDirPath). The timestamp is UTC and fixed-width, so a
listing sorts the session directories by session and the review command finds
the newest record without the user naming a path; the random suffix separates
the sessions that started within one second, so a later session never
overwrites the record an earlier one left for review. The folder holds nothing
but session records, so one directory there is one session. A caller that
wants a determinate place — a test, a session pinned by configuration — names
the directory itself; naming it is the whole selection. The memory layer and
the SQL record remain selectable, and a project that recorded nothing holds no
session directory.

Records accumulate until the user deletes them: the review command reads the
newest one by default, and an older one is browsed by naming its directory.
The record is opened before the process root pivots, so its handle stays on
the host while the session sees the sandbox (see TheoryOfPassthrough).
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

// DefaultRecordPath returns the record a session of the given working directory
// would leave, for the review command: the newest session directory record, the
// database record otherwise. The session directories sort by the UTC timestamp
// their names carry, so the review command finds the latest session without a
// flag. A project that recorded nothing holds neither, and the returned path
// names the database's default place, whose own absence is the command's error
// to report. See TheoryOfDirRecord and TheoryOfSqlUpper.
func DefaultRecordPath(dir string) string {
	if newest, ok := latestSessionDir(dir); ok {
		return newest
	}
	return DefaultDBPath(dir)
}

// latestSessionDir returns the newest session directory record of dir, or
// reports that dir holds none. The session directories live under
// ".tai/sandboxfs" and nothing else does, so the largest directory name is the
// latest session; the name carries the session's UTC timestamp in a
// fixed-width form, and the random suffix only separates the sessions of one
// second, whose order is then arbitrary but stable. See DefaultRecordPath and
// TheoryOfDirRecord.
func latestSessionDir(dir string) (string, bool) {
	sessionsDir := filepath.Join(dir, recordRootName, sessionRecordsName)
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return "", false
	}
	best := ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if name := e.Name(); name > best {
			best = name
		}
	}
	if best == "" {
		return "", false
	}
	return filepath.Join(sessionsDir, best), true
}

// randomRecordSuffix returns the random suffix of one session's record name:
// four random bytes in hexadecimal, so two sessions that start within one
// second get different records. A failing entropy source falls back to the
// clock, which still separates the sessions of one process. See SessionDirPath.
func randomRecordSuffix(now time.Time) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", uint32(now.UnixNano()))
	}
	return hex.EncodeToString(b[:])
}

const (
	// recordRootName names the one directory the tai tool creates in the
	// working directory for its records, so the project tree carries one
	// management directory instead of one dot entry per record kind. See
	// TheoryOfDirRecord.
	recordRootName = ".tai"
	// sessionRecordsName names the folder that holds the session records of
	// one working directory. It holds nothing else, so one directory there is
	// one session. See TheoryOfDirRecord.
	sessionRecordsName = "sandboxfs"
)

// SessionDirPath returns the directory record of one session of the working
// directory: one session directory under the working directory's
// ".tai/sandboxfs", named "<time>.<random>". The timestamp is UTC and
// fixed-width, so a listing sorts the session directories by session and the
// review command finds the newest one without a flag; the random suffix
// separates the sessions that started within one second. A later session
// therefore never overwrites the record an earlier one left for review. See
// TheoryOfDirRecord and TheoryOfDirUpper.
func SessionDirPath(dir string, now time.Time) string {
	return filepath.Join(dir, recordRootName, sessionRecordsName,
		now.UTC().Format("20060102-150405")+"."+randomRecordSuffix(now))
}
