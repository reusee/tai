package sandboxfs

import (
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

const TheoryOfBaseCapture = `
A path's first mutation records the base state it started from: existence,
kind, mode, and — for a regular file or a symbolic link — the content hash
the apply step compares the host against. That record costs the request path
work proportional to the file's size, and a request that runs long holds one
worker of the fixed pool for its whole duration, so enough of them leave no
worker answering. Three rules keep the pause small.

The hash streams through one fixed buffer and stops at maxFileSize. Memory
stays the buffer's, and a path above that bound costs the prefix the store
could hold, never a sequential read of the whole file. The digest is the one
contentHash produces for the same bytes, and the apply reads the host through
the same bound, so both sides of the comparison cover one prefix.

The content bytes and the metadata stay separate. A captured row records the
metadata alone and carries the size sentinel, so a chmod, a utimes, or a
deletion of a file of any size never reads its bytes: Stat asks the base for
the length with one Lstat, and a read falls through to the base. A write
replaces the sentinel with the stored length and the row becomes
content-authoritative; a partial write first materialises the base content, so
the two never disagree on what the row's chunk set means.

The materialised content does not stream past the bound either. A first write
materialises the base bytes into chunks, and a rename or a hard link copies
them into a stored row: the destination is a row the store must hold whole, so
a base file above maxFileSize is refused with EFBIG at the bound. A truncation
captures only the prefix it keeps. A deletion and a permission or time change
record metadata and a hash only, so they stay available on a file of any size,
compared on its prefix alone.

Without the three rules the pause grows with whatever the session touches —
an image, an archive, a database — and a few such pauses together hold every
worker of the pool, until every other process's file I/O waits behind them.
A network client that loads its certificate bundle during a handshake then
pays that pause against a deadline it never set.

Only a node the store can serve is captured: a regular file, a directory, a
link, or a pipe. A device and a socket are refused with EINVAL before
anything opens them. A character device blocks on read until its peer writes
— the controlling terminal blocks until a key arrives — and that read
consumes the bytes it waited for, so a capture that hashed one would swallow
the user's input and hold its worker until the peer writes. See
TheoryOfFuseConcurrency.
`

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

// baseServable reports whether a base node is one the store can record and
// serve: a regular file, a directory, a symbolic link, or a named pipe. A
// character device, a block device, and a socket are none of them, and
// opening a character device blocks until its peer writes — the controlling
// terminal blocks until a key arrives — while the read consumes the bytes it
// waited for. See TheoryOfBaseCapture.
func baseServable(mode os.FileMode) bool {
	return mode.IsRegular() ||
		mode.IsDir() ||
		mode&os.ModeSymlink != 0 ||
		mode&os.ModeNamedPipe != 0
}

// ReadFile returns the content of a regular file: the upper layer's content
// when the session wrote the path, the base's content otherwise. A pipe and a
// symbolic link cannot be read as content and report EINVAL, and a directory
// reports EISDIR, matching the kernel's own answers. A metadata-only row
// reports its content from the base and its size from one Lstat, so a
// chmod-touched path of any size reads without a materialisation. A base
// entry is kind-checked before it is opened, so a pipe path never blocks on
// open. The whole content is bounded by the store's own limit: the callers
// that turn the bytes into a stored row — the hard link and the directory
// rename among them — must meet the bound the store enforces, so a base file
// above maxFileSize reports EFBIG instead of an allocation the store could
// never hold. A reader that wants a window of a large file takes ReadRange,
// which moves only the window it names. The base descriptor answers the open
// and the read with one fstatat and one openat, the same pair a read window
// pays; a store without the descriptor falls back to the os.Root walk
// unchanged. The upper layer's content lives in the chunk table, assembled by
// the stored size. See TheoryOfChunkedContent, TheoryOfBaseFdAccess and
// TheoryOfBaseCapture.
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
		if r.size >= 0 {
			content, err := s.readContent(p, r.size)
			if err != nil {
				return nil, err
			}
			s.auditRead(p)
			return content, nil
		}
		// The row records metadata only; the base holds the content, and the
		// base path below serves it. See TheoryOfBaseCapture.
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
	if info.Size() > maxFileSize {
		return nil, syscall.EFBIG
	}
	b, err := s.baseRoot.ReadFile(rel)
	if err != nil {
		return nil, err
	}
	s.auditRead(p)
	return b, nil
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
// The entries are in name order, which the mount itself does not need: its
// reply carries windows in the base's own order. The cursor is the one
// implementation of the walk and the merge, so this whole-listing API and the
// windowed mount cannot disagree on which entries exist.
// See TheoryOfReadPath and TheoryOfDirectoryStream.
func (s *Store) readDir(p string) (self Attr, entries []DirEntry, err error) {
	c, err := s.openDir(p)
	if err != nil {
		return Attr{}, nil, err
	}
	defer c.Close()
	for {
		batch, err := c.next(dirBatchEntries)
		if err != nil {
			return Attr{}, nil, err
		}
		if len(batch) == 0 {
			break
		}
		entries = append(entries, batch...)
	}
	slices.SortFunc(entries, func(a, b DirEntry) int {
		return strings.Compare(a.Name, b.Name)
	})
	return c.self, entries, nil
}

// ReadDir returns the entries of one sandbox directory in name order. The
// directory's own attribute, which the listing's kind check already reads, is
// not part of the answer: readDir returns both to the one caller that needs
// them together. See TheoryOfReadPath.
func (s *Store) ReadDir(p string) ([]DirEntry, error) {
	_, entries, err := s.readDir(p)
	return entries, err
}

// captureOriginal records the base state of a path the upper layer is about
// to touch for the first time: whether the base holds the path, its kind and
// mode, and — for a regular file or a symbolic link — the content hash the
// apply step compares the host against. The hash streams, so a path of any
// size costs one sequential read of at most the store's bound and never an
// allocation of the file's size. A regular file's row carries the metadata
// sentinel rather than a content length, so a later metadata change touches
// no bytes; a write replaces the sentinel with the stored length. A symbolic
// link's hash carries a tag, so replacing a link with a byte-identical
// regular file stays a change of kind. A named pipe has no content, so its
// kind is recorded as a sentinel the net-change comparison recognises. A base
// node the store cannot serve — a device or a socket — is refused with EINVAL
// before anything opens it, because opening one blocks on its peer's data. A
// path that names the store's own database is refused before the read, so a
// stray write or delete never drags the record through the hash. See
// TheoryOfApplyConflict, TheoryOfBaseCapture and TheoryOfCompletePOSIX.
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
	if !baseServable(info.Mode()) {
		return fileRow{}, fmt.Errorf("%w: the base node is not a file, a directory, a link, or a pipe: %s", syscall.EINVAL, p)
	}
	r.kind = kindOf(info)
	r.mode = info.Mode().Perm()
	r.mtime = info.ModTime()
	r.originalExists = true
	switch r.kind {
	case KindRegular:
		sum, err := hashBaseFile(s.baseRoot, relPath(p))
		if err != nil {
			return fileRow{}, err
		}
		r.originalHash = sum
		// The row is metadata-only: the sentinel says the content still
		// lives in the base. See TheoryOfBaseCapture.
		r.size = -1
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
		// Emptiness is the first window's question, never the whole listing's:
		// a directory of a million entries answers a removal with one window,
		// so the request's duration does not grow with the directory.
		// See TheoryOfDirectoryStream.
		c, err := s.openDir(p)
		if err != nil {
			return err
		}
		first, err := c.next(1)
		c.Close()
		if err != nil {
			return err
		}
		if len(first) > 0 {
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
			if src.size >= 0 {
				content, err = s.readContent(oldPath, src.size)
			} else {
				// A metadata-only source still lives in the base: the rename
				// copies the base's content into the destination's row, which
				// meets the store's own bound. See TheoryOfBaseCapture.
				content, err = s.readBaseContent(oldPath, -1)
			}
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
			// The rename copies the content into the destination's row, so the
			// source meets the store's bound here, the same way a direct write
			// does. See TheoryOfBaseCapture.
			content, err = s.readBaseContent(oldPath, -1)
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
	}
	if r.deleted {
		return ErrNotExist
	}
	// A mode change is not a content change: the row records the bits and a
	// regular file's content stays in the base, so a chmod of a file above
	// the store's bound costs one Lstat and nothing else. POSIX gives chmod
	// no length limit, and a materialisation here would impose one.
	// See TheoryOfBaseCapture.
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
		// originalExists carries that answer.
		// See TheoryOfCompletePOSIX.
		if !r.originalExists {
			return ErrNotExist
		}
	}
	if r.deleted {
		return ErrNotExist
	}
	// A time change is metadata only: the row records the stamp and a regular
	// file's content stays in the base, so a utimes of a file above the
	// store's bound costs one Lstat and nothing else.
	// See TheoryOfBaseCapture.
	r.mtime = mtime
	if err := s.put(r); err != nil {
		return err
	}
	s.audit("utimes", p, mtime.UTC().Format(time.RFC3339Nano))
	return nil
}
