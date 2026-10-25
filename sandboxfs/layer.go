package sandboxfs

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Stat returns the attributes of one sandbox path. It never follows a
// symbolic link: a link reports itself, and the kernel resolves it by
// reading the target and looking up that path, as a filesystem must.
// The upper layer's row is read for metadata only, so a stat on a large
// file never copies its content. The base descriptor answers the base
// lookup with one fstatat; a store without the descriptor falls back to
// the os.Root walk unchanged. See TheoryOfReadPath and TheoryOfBaseFdAccess.
func (s *Store) Stat(p string) (Attr, error) {
	p = Canonical(p)
	a, found, err := s.statRow(p)
	if err != nil {
		return Attr{}, err
	}
	if found {
		return a, nil
	}
	rel := canonicalRel(p)
	if ba, ok, statErr := baseStatAt(s.baseFd, rel); ok {
		if statErr != nil {
			return Attr{}, statErr
		}
		return ba, nil
	}
	info, err := s.baseRoot.Lstat(rel)
	if err != nil {
		return Attr{}, err
	}
	return attrOf(info), nil
}

func attrOf(info os.FileInfo) Attr {
	a := Attr{
		Kind:    kindOf(info),
		Mode:    info.Mode().Perm(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	return a
}

func kindOf(info os.FileInfo) Kind {
	switch {
	case info.IsDir():
		return KindDir
	case info.Mode()&os.ModeSymlink != 0:
		return KindSymlink
	case info.Mode()&os.ModeNamedPipe != 0:
		return KindFIFO
	default:
		return KindRegular
	}
}

// ReadFile returns the content of a regular file: the upper layer's content
// when the session wrote the path, the base's content otherwise. A pipe and
// a symbolic link cannot be read as content and report EINVAL, and a
// directory reports EISDIR, matching the kernel's own answers. A base entry
// is kind-checked before it is opened, so a pipe path never blocks on open.
// The base descriptor answers the open and the read with one fstatat and one
// openat, the same pair a read window pays; a store without the descriptor
// falls back to the os.Root walk unchanged. The upper layer's content lives in
// the chunk table, assembled by the stored size. See TheoryOfChunkedContent and
// TheoryOfBaseFdAccess.
func (s *Store) ReadFile(p string) ([]byte, error) {
	p = Canonical(p)
	r, ok, err := s.row(p)
	if err != nil {
		return nil, err
	}
	if ok {
		if r.deleted {
			return nil, ErrNotExist
		}
		switch r.kind {
		case KindDir:
			return nil, syscall.EISDIR
		case KindSymlink, KindFIFO:
			return nil, syscall.EINVAL
		}
		content, err := s.readContent(p, r.size)
		if err != nil {
			return nil, err
		}
		s.auditRead(p)
		return content, nil
	}
	rel := canonicalRel(p)
	if b, fast, readErr := baseReadFile(s.baseFd, rel); fast {
		if readErr != nil {
			return nil, readErr
		}
		s.auditRead(p)
		return b, nil
	}
	info, err := s.baseRoot.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, syscall.EISDIR
	}
	if !info.Mode().IsRegular() {
		return nil, syscall.EINVAL
	}
	b, err := s.baseRoot.ReadFile(rel)
	if err != nil {
		return nil, err
	}
	s.auditRead(p)
	return b, nil
}

// upperEntries overlays the upper layer's rows onto one listing: a deleted row
// hides its name, and a live row replaces the base's attribute, so a listing
// and a stat never disagree on one path. The listing is one request, so the
// select runs its prepared form. See TheoryOfSandboxFilesystem and
// TheoryOfPreparedStatements.
func (s *Store) upperEntries(p string, entries map[string]Attr) error {
	prefix := p
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	hi := prefix[:len(prefix)-1] + "0"
	rows, err := queryStmt(s.db, s.stmts.selectDir, querySelectDir, prefix, hi)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rp string
		var kind, deleted int
		var mode, mtime int64
		var size, targetSize sql.NullInt64
		if err := rows.Scan(&rp, &kind, &mode, &mtime, &deleted, &size, &targetSize); err != nil {
			return err
		}
		rest := strings.TrimPrefix(rp, prefix)
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		if deleted != 0 {
			delete(entries, rest)
			continue
		}
		at := Attr{
			Kind:    Kind(kind),
			Mode:    os.FileMode(mode),
			ModTime: time.Unix(0, mtime),
		}
		// The size column names content, and only a regular file carries it:
		// a link carries its target's length, and a directory or a pipe
		// carries nothing, matching the same choice in Stat so a listing and
		// a stat never disagree on one path. See TheoryOfChunkedContent.
		switch at.Kind {
		case KindRegular:
			at.Size = size.Int64
		case KindSymlink:
			at.Size = targetSize.Int64
		}
		entries[rest] = at
	}
	return rows.Err()
}

// ReadRange returns the content window [off, off+size) of one path without
// materialising the whole file: the upper layer's window is one indexed query
// over the chunks covering it, and a base file is read with ReadAt. A window
// past the end yields an empty slice, never an error. A window above the
// store's bound cannot describe content the store holds, and the buffer it
// would become is the allocation the bound exists to prevent, so it reports
// EFBIG before anything is reserved. The upper layer's kind and deletion state
// come back in the row query whose stored size clamps the window. The base
// descriptor serves the window with one fstatat, one openat, and one pread; a
// store without the descriptor falls back to the os.Root walk unchanged. See
// TheoryOfCompletePOSIX, TheoryOfReadPath and TheoryOfBaseFdAccess.
func (s *Store) ReadRange(p string, off int64, size int) ([]byte, error) {
	p = Canonical(p)
	if off < 0 || size < 0 {
		return nil, syscall.EINVAL
	}
	if size > maxFileSize {
		return nil, syscall.EFBIG
	}
	chunk, kind, found, err := s.readUpperRange(p, off, size)
	if err != nil {
		return nil, err
	}
	if found {
		switch kind {
		case KindDir:
			return nil, syscall.EISDIR
		case KindSymlink, KindFIFO:
			return nil, syscall.EINVAL
		}
		s.auditRead(p)
		return chunk, nil
	}
	rel := canonicalRel(p)
	if b, ok, readErr := baseReadAt(s.baseFd, rel, off, size); ok {
		if readErr != nil {
			return nil, readErr
		}
		s.auditRead(p)
		return b, nil
	}
	info, err := s.baseRoot.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, syscall.EISDIR
	}
	if !info.Mode().IsRegular() {
		return nil, syscall.EINVAL
	}
	f, err := s.baseRoot.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, size)
	n, readErr := f.ReadAt(buf, off)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	s.auditRead(p)
	return buf[:n], nil
}

// readDir returns the directory's own attribute beside its entries: a listing
// carries "." and "..", both of which come from the store, and a separate stat
// of the directory would repeat the base lookup the kind check already paid.
// The entries are in name order. See TheoryOfReadPath.
func (s *Store) readDir(p string) (self Attr, entries []DirEntry, err error) {
	p = Canonical(p)
	self, err = s.Stat(p)
	if err != nil {
		return Attr{}, nil, err
	}
	if self.Kind != KindDir {
		return Attr{}, nil, syscall.ENOTDIR
	}
	list, err := s.baseEntries(p)
	if err != nil {
		return Attr{}, nil, err
	}
	// An empty upper layer holds no row to overlay or hide, so the listing is
	// the base's own and the merge below would cost a map and two slices for
	// entries no row can change. See TheoryOfUpperAbsentCache.
	if s.upperEmpty.Load() {
		slices.SortFunc(list, func(a, b DirEntry) int {
			return strings.Compare(a.Name, b.Name)
		})
		return self, list, nil
	}
	merged := make(map[string]Attr, len(list))
	for _, e := range list {
		merged[e.Name] = e.Attr
	}
	if err := s.upperEntries(p, merged); err != nil {
		return Attr{}, nil, err
	}
	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]DirEntry, 0, len(names))
	for _, name := range names {
		out = append(out, DirEntry{Name: name, Attr: merged[name]})
	}
	return self, out, nil
}

// ReadDir returns the entries of one sandbox directory in name order. The
// directory's own attribute, which the listing's kind check already reads, is
// not part of the answer: readDir returns both to the one caller that needs
// them together. See TheoryOfReadPath.
func (s *Store) ReadDir(p string) ([]DirEntry, error) {
	_, entries, err := s.readDir(p)
	return entries, err
}

// baseEntries returns the base layer's listing of one canonical directory, in
// the order the base reported it. A path the base does not hold as a directory
// contributes no entry: a missing path, a regular file, and a symbolic link are
// the upper layer's to answer. The base descriptor answers the whole listing
// with one openat and one fstatat per entry, so a walk of a large tree pays no
// component-by-component guard per directory and no path-based stat per entry.
// A store without the descriptor falls back to the os.Root walk unchanged.
// Two callers share this one walk — the merged listing and the listing of a
// session that wrote nothing — so the two can never disagree on which entries
// the base holds. See TheoryOfBaseFdAccess and TheoryOfReadPath.
func (s *Store) baseEntries(p string) ([]DirEntry, error) {
	rel := canonicalRel(p)
	list, fast, err := baseReadDir(s.baseFd, rel)
	if fast {
		switch {
		case err == nil:
			return list, nil
		case errors.Is(err, syscall.ENOENT),
			errors.Is(err, syscall.ENOTDIR),
			errors.Is(err, syscall.ELOOP):
			return nil, nil
		default:
			return nil, err
		}
	}
	info, err := s.baseRoot.Lstat(rel)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	f, err := s.baseRoot.Open(rel)
	if err != nil {
		return nil, err
	}
	des, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, DirEntry{Name: de.Name(), Attr: attrOf(info)})
	}
	return out, nil
}

// captureOriginal records the base state of a path the upper layer is about
// to touch for the first time: whether the base holds the path, its kind and
// mode, and — for a regular file or a symbolic link — the content hash the
// apply step compares the host against. A symbolic link's hash carries a tag,
// so replacing a link with a byte-identical regular file stays a change of
// kind. A named pipe has no content, so its kind is recorded as a sentinel the
// net-change comparison recognises. A path that names the store's own database
// is refused before the read, so a stray write or delete never drags the
// record through captureOriginal's whole-file hash.
// See TheoryOfApplyConflict and TheoryOfCompletePOSIX.
func (s *Store) captureOriginal(p string) (fileRow, error) {
	if s.isOwnDBPath(p) {
		return fileRow{}, fmt.Errorf("%w: the sandbox database is never a sandbox target: %s", os.ErrPermission, p)
	}
	r := fileRow{path: p, mode: 0o644}
	info, err := s.baseRoot.Lstat(relPath(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r, nil
		}
		return fileRow{}, err
	}
	r.kind = kindOf(info)
	r.mode = info.Mode().Perm()
	r.mtime = info.ModTime()
	r.originalExists = true
	switch r.kind {
	case KindRegular:
		content, err := s.baseRoot.ReadFile(relPath(p))
		if err != nil {
			return fileRow{}, err
		}
		r.originalHash = contentHash(content)
	case KindSymlink:
		target, err := s.baseRoot.Readlink(relPath(p))
		if err != nil {
			return fileRow{}, err
		}
		r.target = target
		r.originalHash = symlinkOriginalPrefix + contentHash([]byte(target))
	case KindFIFO:
		r.originalHash = fifoOriginalHash
	}
	return r, nil
}

func (s *Store) checkParent(p string) error {
	parent := path.Dir(p)
	a, err := s.Stat(parent)
	if err != nil {
		return err
	}
	if a.Kind != KindDir {
		return syscall.ENOTDIR
	}
	return nil
}

func (s *Store) WriteFile(p string, content []byte, perm os.FileMode) error {
	p = Canonical(p)
	if p == "/" {
		return syscall.EISDIR
	}
	if err := s.checkWrite(p); err != nil {
		return err
	}
	if err := s.checkParent(p); err != nil {
		return err
	}
	if a, err := s.Stat(p); err == nil {
		switch a.Kind {
		case KindDir:
			return syscall.EISDIR
		case KindFIFO:
			return syscall.EINVAL
		}
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
	}
	// perm names the mode the write gives the file, whichever branch above
	// supplied the row: a rename gives its destination the source's mode even
	// when an earlier whiteout already left a row there. The mode is applied
	// as given, 000 included, so no layer substitutes a default for what the
	// caller chose. See TheoryOfCompletePOSIX.
	r.mode = perm
	r.path = p
	r.kind = KindRegular
	r.deleted = false
	r.target = ""
	r.mtime = time.Now()
	if err := s.putContent(r, content); err != nil {
		return err
	}
	s.audit("write", p, fmt.Sprintf("%d bytes", len(content)))
	return nil
}

func (s *Store) Mkdir(p string, perm os.FileMode) error {
	p = Canonical(p)
	if p == "/" {
		return ErrExist
	}
	if err := s.checkWrite(p); err != nil {
		return err
	}
	if err := s.checkParent(p); err != nil {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if ok && !r.deleted {
		return ErrExist
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
		if r.originalExists {
			return ErrExist
		}
	}
	// The mode is applied as given, 000 included: a caller that means to
	// create a directory with no access bits chose exactly that, and a default
	// here would hand the store bits the caller never asked for. See
	// TheoryOfCompletePOSIX.
	r.path = p
	r.kind = KindDir
	r.deleted = false
	r.target = ""
	r.mode = perm
	r.mtime = time.Now()
	// A directory carries no content, so the size column reports zero: a
	// path that was a file keeps a stale length otherwise, and a listing
	// would report it beside a stat that says zero. See TheoryOfChunkedContent.
	r.size = 0
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("mkdir", p, "")
	return nil
}

func (s *Store) Symlink(target, p string) error {
	p = Canonical(p)
	// A link names a target, and an empty target names nothing: POSIX refuses
	// symlink(2) with ENOENT, and a store that recorded the empty link would
	// hand the apply a change the host can never carry out.
	// See TheoryOfCompletePOSIX.
	if target == "" {
		return syscall.ENOENT
	}
	if err := s.checkWrite(p); err != nil {
		return err
	}
	if err := s.checkParent(p); err != nil {
		return err
	}
	if _, err := s.Stat(p); err == nil {
		return ErrExist
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
	}
	r.path = p
	r.kind = KindSymlink
	r.deleted = false
	r.target = target
	r.mtime = time.Now()
	// The stored size column names content the link never has, so a path that
	// was a file must not keep its length behind the new link: the size a
	// reader sees comes from the target. See TheoryOfChunkedContent.
	r.size = 0
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("symlink", p, target)
	return nil
}

func (s *Store) Mkfifo(p string, mode os.FileMode) error {
	p = Canonical(p)
	if err := s.checkWrite(p); err != nil {
		return err
	}
	if err := s.checkParent(p); err != nil {
		return err
	}
	if _, err := s.Stat(p); err == nil {
		return ErrExist
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
	}
	// The mode is applied as given, 000 included: a pipe with no access bits
	// is a choice, not an absent mode. See TheoryOfCompletePOSIX.
	r.path = p
	r.kind = KindFIFO
	r.deleted = false
	r.target = ""
	r.mode = mode
	r.mtime = time.Now()
	// A pipe carries no content, so the size column reports zero: a path that
	// was a file keeps a stale length otherwise. See TheoryOfCompletePOSIX.
	r.size = 0
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("mkfifo", p, "")
	return nil
}

// Readlink returns the target of a symbolic link.
func (s *Store) Readlink(p string) (string, error) {
	p = Canonical(p)
	r, ok, err := s.row(p)
	if err != nil {
		return "", err
	}
	if ok {
		if r.deleted {
			return "", ErrNotExist
		}
		if r.kind != KindSymlink {
			return "", syscall.EINVAL
		}
		return r.target, nil
	}
	return s.baseRoot.Readlink(relPath(p))
}

func (s *Store) Remove(p string) error {
	p = Canonical(p)
	if p == "/" {
		return syscall.EBUSY
	}
	if err := s.checkWrite(p); err != nil {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if ok && r.deleted {
		return ErrNotExist
	}
	a, err := s.Stat(p)
	if err != nil {
		return err
	}
	if a.Kind == KindDir {
		entries, err := s.ReadDir(p)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return ErrNotEmpty
		}
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
	}
	r.path = p
	r.deleted = true
	r.target = ""
	r.mtime = time.Now()
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("delete", p, "")
	return nil
}

func (s *Store) Rename(oldPath, newPath string) error {
	oldPath = Canonical(oldPath)
	newPath = Canonical(newPath)
	if oldPath == "/" || newPath == "/" {
		return syscall.EBUSY
	}
	if oldPath == newPath {
		// rename(2) resolves the source before it treats the no-op, so a
		// missing source reports ENOENT whatever the two names share. An
		// existing path renamed to itself stays a successful no-op.
		// See TheoryOfCompletePOSIX.
		if _, err := s.Stat(oldPath); err != nil {
			return err
		}
		return nil
	}
	if err := s.checkWrite(oldPath); err != nil {
		return err
	}
	if err := s.checkWrite(newPath); err != nil {
		return err
	}
	if a, err := s.Stat(oldPath); err == nil && a.Kind == KindDir {
		return s.renameDir(oldPath, newPath)
	}
	var kind Kind
	var mode os.FileMode
	var mtime time.Time
	var content []byte
	var target string
	src, ok, err := s.row(oldPath)
	if err != nil {
		return err
	}
	if ok {
		if src.deleted {
			return ErrNotExist
		}
		kind, mode, target, mtime = src.kind, src.mode, src.target, src.mtime
		if kind == KindRegular {
			content, err = s.readContent(oldPath, src.size)
			if err != nil {
				return err
			}
		}
	} else {
		info, err := s.baseRoot.Lstat(relPath(oldPath))
		if err != nil {
			return err
		}
		kind = kindOf(info)
		mode = info.Mode().Perm()
		mtime = info.ModTime()
		switch kind {
		case KindRegular:
			content, err = s.baseRoot.ReadFile(relPath(oldPath))
			if err != nil {
				return err
			}
		case KindSymlink:
			target, err = s.baseRoot.Readlink(relPath(oldPath))
			if err != nil {
				return err
			}
		}
	}
	if err := s.checkParent(newPath); err != nil {
		return err
	}
	if a, err := s.Stat(newPath); err == nil {
		if a.Kind == KindDir {
			return syscall.EISDIR
		}
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	dst, dstOK, err := s.row(newPath)
	if err != nil {
		return err
	}
	if !dstOK {
		dst, err = s.captureOriginal(newPath)
		if err != nil {
			return err
		}
	}
	dst.path = newPath
	dst.kind = kind
	dst.mode = mode
	dst.deleted = false
	dst.target = target
	// Rename moves the node, so the file's recorded time moves with it:
	// POSIX gives rename no mtime effect, and a toolchain that renames a
	// file must not see its staleness jump. See TheoryOfCompletePOSIX.
	dst.mtime = mtime
	if err := s.putContent(dst, content); err != nil {
		return err
	}
	rem, remOK, err := s.row(oldPath)
	if err != nil {
		return err
	}
	if !remOK {
		rem, err = s.captureOriginal(oldPath)
		if err != nil {
			return err
		}
	}
	rem.path = oldPath
	rem.deleted = true
	rem.target = ""
	rem.mtime = time.Now()
	if err := s.put(rem); err != nil {
		return err
	}
	s.audit("rename", oldPath, newPath)
	return nil
}

func (s *Store) renameDir(oldPath, newPath string) error {
	if strings.HasPrefix(newPath, oldPath+"/") {
		return syscall.EINVAL
	}
	destinationExists := false
	if a, err := s.Stat(newPath); err == nil {
		if a.Kind != KindDir {
			return syscall.ENOTDIR
		}
		entries, err := s.ReadDir(newPath)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return ErrNotEmpty
		}
		destinationExists = true
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	if err := s.checkParent(newPath); err != nil {
		return err
	}
	dst := func(p string) string {
		return newPath + strings.TrimPrefix(p, oldPath)
	}
	type entry struct {
		path    string
		attr    Attr
		content []byte
		target  string
	}
	var entries []entry
	var walk func(p string) error
	walk = func(p string) error {
		a, err := s.Stat(p)
		if err != nil {
			return err
		}
		// Both endpoints are mutations — the destination write and the
		// source whiteout — and both are asked before any of them happens,
		// so a refused write leaves the tree untouched.
		// See TheoryOfWritePolicy.
		if err := s.checkWrite(p); err != nil {
			return err
		}
		if err := s.checkWrite(dst(p)); err != nil {
			return err
		}
		e := entry{path: p, attr: a}
		switch a.Kind {
		case KindRegular:
			if e.content, err = s.ReadFile(p); err != nil {
				return err
			}
		case KindSymlink:
			if e.target, err = s.Readlink(p); err != nil {
				return err
			}
		}
		entries = append(entries, e)
		if a.Kind == KindDir {
			list, err := s.ReadDir(p)
			if err != nil {
				return err
			}
			for _, de := range list {
				if err := walk(path.Join(p, de.Name)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(oldPath); err != nil {
		return err
	}
	if destinationExists {
		if err := s.Remove(newPath); err != nil {
			return err
		}
	}
	// Materialize the destination parent-first: the walk visits parent
	// before child, so entries is already in insertion order.
	for _, e := range entries {
		target := dst(e.path)
		switch e.attr.Kind {
		case KindDir:
			if err := s.Mkdir(target, e.attr.Mode); err != nil {
				return err
			}
		case KindRegular:
			if err := s.WriteFile(target, e.content, e.attr.Mode); err != nil {
				return err
			}
		case KindSymlink:
			if err := s.Symlink(e.target, target); err != nil {
				return err
			}
		case KindFIFO:
			if err := s.Mkfifo(target, e.attr.Mode); err != nil {
				return err
			}
		}
		// The move keeps the node's time: POSIX gives rename no mtime
		// effect, and the creations above stamp the session's clock.
		// See TheoryOfCompletePOSIX.
		if err := s.SetTimes(target, e.attr.ModTime); err != nil {
			return err
		}
	}
	// Whiteout the source deepest-first, so each Remove sees an empty
	// directory.
	for i := len(entries) - 1; i >= 0; i-- {
		if err := s.Remove(entries[i].path); err != nil {
			return err
		}
	}
	s.audit("rename-dir", oldPath, newPath)
	return nil
}

func (s *Store) SetMode(p string, mode os.FileMode) error {
	p = Canonical(p)
	if err := s.checkWrite(p); err != nil {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
		// A mode change names a path that must exist. captureOriginal leaves
		// kind at its zero value, which is KindRegular, so originalExists is
		// the only honest test of the base's verdict.
		// See TheoryOfCompletePOSIX.
		if !r.originalExists {
			return ErrNotExist
		}
		if r.kind == KindRegular {
			// The materialisation keeps the base file's mode, so the mode-only
			// change below starts from the bits the path already carried.
			// See TheoryOfCompletePOSIX.
			r, err = s.materializeContent(p, -1, r.mode)
			if err != nil {
				return err
			}
		}
	}
	if r.deleted {
		return ErrNotExist
	}
	// A mode change is not a content change: it records the bits and leaves
	// the stored mtime as the base recorded it, because POSIX gives chmod no
	// mtime effect and a toolchain reads mtime to judge staleness. See
	// TheoryOfCompletePOSIX.
	r.mode = mode.Perm()
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("chmod", p, mode.Perm().String())
	return nil
}

// CanWrite reports whether the store's write policy admits a mutation of p,
// without performing any change. The FUSE layer uses it to refuse a
// write-mode open at open time, so the caller sees EACCES on open rather
// than on its first write. See TheoryOfWritePolicy.
func (s *Store) CanWrite(p string) error {
	return s.checkWrite(Canonical(p))
}

// Link creates a hard link at newPath sharing oldPath's content. POSIX allows
// hard links only to regular files and symbolic links; directories report
// EPERM. The store is path-addressed with no shared inode, so the link copies
// the current content into a new row that then evolves independently, matching
// the observable behavior a coding agent expects from a hard link: same
// content at both paths until either is written. The copied content lands as
// the destination's chunks. See TheoryOfChunkedContent.
func (s *Store) Link(oldPath, newPath string) error {
	oldPath = Canonical(oldPath)
	newPath = Canonical(newPath)
	if err := s.checkWrite(newPath); err != nil {
		return err
	}
	a, err := s.Stat(oldPath)
	if err != nil {
		return err
	}
	if a.Kind == KindDir {
		return syscall.EPERM
	}
	if _, err := s.Stat(newPath); err == nil {
		return ErrExist
	} else if !errors.Is(err, ErrNotExist) {
		return err
	}
	if err := s.checkParent(newPath); err != nil {
		return err
	}
	r, ok, err := s.row(newPath)
	if err != nil {
		return err
	}
	if !ok {
		r, err = s.captureOriginal(newPath)
		if err != nil {
			return err
		}
	}
	var content []byte
	var target string
	switch a.Kind {
	case KindRegular:
		content, err = s.ReadFile(oldPath)
		if err != nil {
			return err
		}
	case KindSymlink:
		target, err = s.Readlink(oldPath)
		if err != nil {
			return err
		}
	}
	r.path = newPath
	r.kind = a.Kind
	r.mode = a.Mode
	r.deleted = false
	r.target = target
	// A hard link shares the node, so the new name reports the source's own
	// time: POSIX gives link no mtime effect on the file.
	// See TheoryOfCompletePOSIX.
	r.mtime = a.ModTime
	if err := s.putContent(r, content); err != nil {
		return err
	}
	s.audit("link", oldPath, newPath)
	return nil
}

func (s *Store) SetTimes(p string, mtime time.Time) error {
	p = Canonical(p)
	if err := s.checkWrite(p); err != nil {
		return err
	}
	r, ok, err := s.row(p)
	if err != nil {
		return err
	}
	if !ok {
		r, err = s.captureOriginal(p)
		if err != nil {
			return err
		}
		// A time change names a path that must exist; see SetMode for why
		// originalExists carries that answer. The materialisation keeps the
		// base's mode, because a time-only change must not rewrite the
		// permission bits. See TheoryOfCompletePOSIX.
		if !r.originalExists {
			return ErrNotExist
		}
		if r.kind == KindRegular {
			r, err = s.materializeContent(p, -1, r.mode)
			if err != nil {
				return err
			}
		}
	}
	if r.deleted {
		return ErrNotExist
	}
	r.mtime = mtime
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("utimes", p, mtime.UTC().Format(time.RFC3339Nano))
	return nil
}
