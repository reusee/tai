package sandboxfs

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// memoryUpper keeps the upper layer in process memory: the same metadata rows
// and content chunks the sqlite layer stores, held in maps under one mutex.
// The mutex is the layer's atomicity: every composite operation runs whole
// while it is held, so a reader never sees a row and a content that disagree.
// See TheoryOfUpperLayer.
type memoryUpper struct {
	mu     sync.Mutex
	files  map[string]fileRow
	chunks map[string]map[int64][]byte
	ops    []Op
}

var _ upperStore = (*memoryUpper)(nil)

func newMemoryUpper() *memoryUpper {
	return &memoryUpper{
		files:  make(map[string]fileRow),
		chunks: make(map[string]map[int64][]byte),
	}
}

func (m *memoryUpper) row(p string) (fileRow, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[p]
	return r, ok, nil
}

func (m *memoryUpper) window(p string, off int64, size int) ([]byte, Kind, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[p]
	if !ok {
		return nil, 0, false, nil
	}
	if r.deleted {
		return nil, 0, true, ErrNotExist
	}
	if r.kind != KindRegular {
		return nil, r.kind, true, nil
	}
	if r.size < 0 {
		// The row records metadata alone: the base holds the content.
		// See TheoryOfBaseCapture.
		return nil, 0, false, nil
	}
	end := min(off+int64(size), r.size)
	if off >= end {
		return []byte{}, KindRegular, true, nil
	}
	return m.windowLocked(p, off, end), KindRegular, true, nil
}

func (m *memoryUpper) content(p string, off int64, size int) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[p]
	if !ok || r.deleted || r.kind != KindRegular || r.size < 0 {
		return nil, nil
	}
	end := min(off+int64(size), r.size)
	if off >= end {
		return []byte{}, nil
	}
	return m.windowLocked(p, off, end), nil
}

// windowLocked assembles the content window [off, end) from the stored chunks.
// An absent chunk reads as zeros, so a window past the stored chunks still
// answers its full length. The caller holds the lock.
// See TheoryOfChunkedContent.
func (m *memoryUpper) windowLocked(p string, off, end int64) []byte {
	out := make([]byte, end-off)
	chunkMap := m.chunks[p]
	for idx := off / contentChunkSize; idx <= (end-1)/contentChunkSize; idx++ {
		data := chunkMap[idx]
		if len(data) == 0 {
			continue
		}
		base := idx * contentChunkSize
		from := max(off, base)
		to := min(end, base+int64(len(data)))
		if to <= from {
			continue
		}
		copy(out[from-off:to-off], data[from-base:to-base])
	}
	return out
}

func (m *memoryUpper) rowsUnder(dir string) ([]fileRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := dir
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var out []fileRow
	for p, r := range m.files {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		if strings.Contains(p[len(prefix):], "/") {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *memoryUpper) allRows() ([]fileRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]fileRow, 0, len(m.files))
	for _, r := range m.files {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

func (m *memoryUpper) putRow(r fileRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[r.path] = r
	if r.deleted || r.kind != KindRegular {
		delete(m.chunks, r.path)
	}
	return nil
}

func (m *memoryUpper) replaceContent(r fileRow, content []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.size = int64(len(content))
	m.files[r.path] = r
	m.storeContentLocked(r.path, content)
	return nil
}

func (m *memoryUpper) createContent(r fileRow, content []byte) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cur, ok := m.files[r.path]; ok && !cur.deleted && cur.kind == KindRegular && cur.size >= 0 {
		// Another writer's live regular row carries the content, and replacing
		// it would drop what that writer stored. See TheoryOfWriteSplice.
		return false, nil
	}
	r.size = int64(len(content))
	m.files[r.path] = r
	m.storeContentLocked(r.path, content)
	return true, nil
}

func (m *memoryUpper) splice(p string, off int64, data []byte, mtime time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[p]
	if !ok || r.deleted || r.kind != KindRegular || r.size < 0 {
		return errNoLiveRegularRow
	}
	end := off + int64(len(data))
	if end > r.size {
		r.size = end
	}
	r.mtime = mtime
	m.files[p] = r
	chunkMap := m.chunks[p]
	if chunkMap == nil {
		chunkMap = make(map[int64][]byte)
		m.chunks[p] = chunkMap
	}
	for idx := off / contentChunkSize; idx <= (end-1)/contentChunkSize; idx++ {
		base := idx * contentChunkSize
		from := max(off, base)
		to := min(end, base+contentChunkSize)
		lo, hi := from-base, to-base
		payload := data[from-off : to-off]
		if lo == 0 && hi == contentChunkSize {
			// The payload covers the chunk whole, so its own bytes are the
			// stored bytes: no read of the old chunk, and the copy below is
			// the one the layer owns.
			chunkMap[idx] = slices.Clone(payload)
			continue
		}
		existing := chunkMap[idx]
		buf := make([]byte, max(int64(len(existing)), hi))
		copy(buf, existing)
		copy(buf[lo:hi], payload)
		chunkMap[idx] = buf
	}
	return nil
}

func (m *memoryUpper) truncate(p string, size int64, mtime time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.files[p]
	if !ok || r.deleted || r.kind != KindRegular {
		return ErrNotExist
	}
	keep := (size + contentChunkSize - 1) / contentChunkSize
	chunkMap := m.chunks[p]
	for idx := range chunkMap {
		if idx >= keep {
			delete(chunkMap, idx)
		}
	}
	if rem := size % contentChunkSize; rem != 0 && keep > 0 {
		if data, ok := chunkMap[keep-1]; ok && int64(len(data)) > rem {
			chunkMap[keep-1] = slices.Clone(data[:rem])
		}
	}
	if len(chunkMap) == 0 {
		delete(m.chunks, p)
	}
	r.size = size
	r.mtime = mtime
	m.files[p] = r
	return nil
}

func (m *memoryUpper) drop(p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, p)
	delete(m.chunks, p)
	return nil
}

func (m *memoryUpper) writeAudit(ops []Op) error {
	if len(ops) == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ops = append(m.ops, ops...)
	return nil
}

func (m *memoryUpper) auditRows() ([]Op, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.ops), nil
}

func (m *memoryUpper) close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = nil
	m.chunks = nil
	m.ops = nil
	return nil
}

// storeContentLocked replaces one path's whole content with content's chunks.
// Every chunk is copied: the caller's slice may alias a reused request buffer.
// An empty content leaves no chunk at all, so an empty file stores nothing.
// The caller holds the lock. See TheoryOfChunkedContent.
func (m *memoryUpper) storeContentLocked(p string, content []byte) {
	if len(content) == 0 {
		delete(m.chunks, p)
		return
	}
	chunkMap := make(map[int64][]byte, (len(content)+contentChunkSize-1)/contentChunkSize)
	for off := 0; off < len(content); off += contentChunkSize {
		chunkMap[int64(off/contentChunkSize)] = slices.Clone(content[off:min(off+contentChunkSize, len(content))])
	}
	m.chunks[p] = chunkMap
}
