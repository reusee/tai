package sandboxfs

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// newLayerStores opens one store per upper layer over the same base directory,
// so a test can run one operation sequence against both and compare what a
// caller sees. See TheoryOfUpperLayer.
func newLayerStores(t *testing.T) map[string]*Store {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	mem, err := OpenMemory(base)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := OpenDir(filepath.Join(dir, "record"), base)
	if err != nil {
		t.Fatal(err)
	}
	stores := map[string]*Store{"memory": mem, "dir": rec}
	t.Cleanup(func() {
		for _, s := range stores {
			_ = s.Close()
		}
	})
	return stores
}

// layerView renders everything a caller can observe about one store: the
// attribute of each named path, the content of each regular file, the target of
// each link, and a listing of each named path. The time is left out because
// each layer stamps its own clock.
func layerView(t *testing.T, s *Store, paths []string) string {
	t.Helper()
	var b bytes.Buffer
	for _, p := range paths {
		a, err := s.Stat(p)
		if err != nil {
			fmt.Fprintf(&b, "stat %s: %v\n", p, err)
			continue
		}
		fmt.Fprintf(&b, "stat %s: kind %d mode %o size %d\n", p, a.Kind, a.Mode, a.Size)
		switch a.Kind {
		case KindRegular:
			content, err := s.ReadFile(p)
			if err != nil {
				fmt.Fprintf(&b, "read %s: %v\n", p, err)
				continue
			}
			fmt.Fprintf(&b, "read %s: %d bytes %s\n", p, len(content), contentHash(content)[:16])
		case KindSymlink:
			target, err := s.Readlink(p)
			if err != nil {
				fmt.Fprintf(&b, "readlink %s: %v\n", p, err)
				continue
			}
			fmt.Fprintf(&b, "readlink %s: %s\n", p, target)
		}
	}
	for _, dir := range paths {
		entries, err := s.ReadDir(dir)
		if err != nil {
			fmt.Fprintf(&b, "list %s: %v\n", dir, err)
			continue
		}
		for _, e := range entries {
			fmt.Fprintf(&b, "entry %s/%s: kind %d mode %o size %d\n",
				dir, e.Name, e.Attr.Kind, e.Attr.Mode, e.Attr.Size)
		}
	}
	return b.String()
}

// TestLayersAgreeOnOneOperationSequence protects TheoryOfUpperLayer: both
// backends implement one contract, so one operation sequence must leave one
// observable state whatever backend holds the session's mutations. A divergence
// here is a defect in whichever layer disagrees, and the review reads a
// different answer depending on where the session recorded itself.
func TestLayersAgreeOnOneOperationSequence(t *testing.T) {
	stores := newLayerStores(t)
	big := bytes.Repeat([]byte("0123456789abcdef"), 1000)
	ops := []struct {
		name string
		run  func(s *Store) error
	}{
		{"write /a", func(s *Store) error { return s.WriteFile("/a", []byte("0123456789"), 0o644) }},
		{"splice into /a", func(s *Store) error { return s.SpliceFile("/a", 3, []byte("XYZ"), 0o644) }},
		{"shrink /a", func(s *Store) error { return s.TruncateFile("/a", 5, 0o644) }},
		{"grow /a", func(s *Store) error { return s.TruncateFile("/a", 20, 0o644) }},
		{"splice past the stored end", func(s *Store) error { return s.SpliceFile("/a", 18, []byte("PQ"), 0o644) }},
		{"write big /b", func(s *Store) error { return s.WriteFile("/b", big, 0o600) }},
		{"splice across a chunk", func(s *Store) error {
			return s.SpliceFile("/b", contentChunkSize-3, []byte("abcdef"), 0o600)
		}},
		{"shrink /b inside a chunk", func(s *Store) error {
			return s.TruncateFile("/b", contentChunkSize+5, 0o600)
		}},
		{"mkdir /d", func(s *Store) error { return s.Mkdir("/d", 0o700) }},
		{"write /d/f", func(s *Store) error { return s.WriteFile("/d/f", []byte("f"), 0o600) }},
		{"symlink /l", func(s *Store) error { return s.Symlink("a", "/l") }},
		{"rename /a to /c", func(s *Store) error { return s.Rename("/a", "/c") }},
		{"link /c to /h", func(s *Store) error { return s.Link("/c", "/h") }},
		{"remove /d/f", func(s *Store) error { return s.Remove("/d/f") }},
		{"chmod /b", func(s *Store) error { return s.SetMode("/b", 0o640) }},
	}
	paths := []string{"/", "/a", "/b", "/c", "/d", "/d/f", "/h", "/l"}
	for _, op := range ops {
		for name, s := range stores {
			if err := op.run(s); err != nil {
				t.Fatalf("%s on the %s layer: %v", op.name, name, err)
			}
		}
		views := make(map[string]string, len(stores))
		for name, s := range stores {
			views[name] = layerView(t, s, paths)
		}
		ref := views["memory"]
		for _, name := range []string{"dir"} {
			if views[name] != ref {
				t.Fatalf("after %q the %s and memory layers disagree:\n--- memory\n%s\n--- %s\n%s",
					op.name, name, ref, name, views[name])
			}
		}
	}
}

// FuzzLayersAgreeOnRandomOperationSequence turns the fuzz input into a
// deterministic operation sequence and runs it against every upper layer. The
// operation outcome and the observable tree must agree after every step, so a
// divergence identifies a backend defect without duplicating the store's union
// logic in a second model. See TheoryOfUpperLayer.
func FuzzLayersAgreeOnRandomOperationSequence(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{0, 1, 0, 32, 0, 0x20})
	f.Add([]byte{
		3, 4, 0, 0, 0, 0,
		0, 5, 0, 8, 0, 0,
		6, 4, 8, 0, 0, 0,
		9, 9, 0, 0, 1, 0,
	})
	f.Add([]byte{
		1, 1, 0, 0, 0, 0x86,
		2, 1, 0, 0, 0, 0x80,
	})
	f.Add(bytes.Repeat([]byte{0, 1, 2, 3, 4, 5}, 8))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 256 {
			input = input[:256]
		}
		stores := newLayerStores(t)
		seedFuzzLayerBase(t, stores["memory"].Base())
		for i, op := range fuzzLayerOperations(input) {
			if err := checkFuzzLayerOperation(t, stores, op); err != nil {
				t.Fatalf("input %x, operation %d, %s: %v", input, i, op, err)
			}
		}
	})
}

func seedFuzzLayerBase(t *testing.T, base string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(base, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("base-"), 1000)
	if err := os.WriteFile(filepath.Join(base, "a"), content, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "d", "existing"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkFuzzLayerOperation(t *testing.T, stores map[string]*Store, op fuzzLayerOperation) error {
	t.Helper()
	outcomes := make(map[string]string, len(stores))
	for name, s := range stores {
		outcomes[name] = fuzzLayerError(op.run(s))
	}
	for _, name := range []string{"dir"} {
		if outcomes[name] != outcomes["memory"] {
			return fmt.Errorf("operation outcome: memory=%s, %s=%s",
				outcomes["memory"], name, outcomes[name])
		}
	}
	views := make(map[string]string, len(stores))
	for name, s := range stores {
		views[name] = layerView(t, s, fuzzLayerPaths)
	}
	for _, name := range []string{"dir"} {
		if views[name] != views["memory"] {
			return fmt.Errorf("state:\n--- memory\n%s\n--- %s\n%s",
				views["memory"], name, views[name])
		}
	}
	return nil
}

func fuzzLayerError(err error) string {
	if err == nil {
		return "ok"
	}
	if errors.Is(err, ErrNotExist) {
		return "not exist"
	}
	if errors.Is(err, ErrExist) {
		return "exist"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return fmt.Sprintf("errno %d", errno)
	}
	return fmt.Sprintf("%T", err)
}

func (o fuzzLayerOperation) String() string {
	return fmt.Sprintf("%s from=%q to=%q offset=%d bytes=%d mode=%#o mtime=%s",
		o.name, o.from, o.to, o.offset, len(o.data), o.mode, o.mtime.Format(time.RFC3339Nano))
}

func (o fuzzLayerOperation) run(s *Store) error {
	switch o.name {
	case "write":
		return s.WriteFile(o.from, o.data, o.mode)
	case "splice":
		return s.SpliceFile(o.from, o.offset, o.data, o.mode)
	case "truncate":
		return s.TruncateFile(o.from, o.offset, o.mode)
	case "mkdir":
		return s.Mkdir(o.from, o.mode)
	case "mkfifo":
		return s.Mkfifo(o.from, o.mode)
	case "remove":
		return s.Remove(o.from)
	case "rename":
		return s.Rename(o.from, o.to)
	case "link":
		return s.Link(o.from, o.to)
	case "symlink":
		return s.Symlink(o.to, o.from)
	case "chmod":
		return s.SetMode(o.from, o.mode)
	case "utimes":
		return s.SetTimes(o.from, o.mtime)
	}
	panic("unknown fuzz layer operation: " + o.name)
}

func fuzzLayerOperations(input []byte) []fuzzLayerOperation {
	names := [...]string{
		"write", "splice", "truncate", "mkdir", "mkfifo", "remove",
		"rename", "link", "symlink", "chmod", "utimes",
	}
	linkTargets := [...]string{"", "a", "/a", "../a", "missing"}
	var operations []fuzzLayerOperation
	for step := 0; len(input) >= 6; step++ {
		raw := input[:6]
		input = input[6:]
		value := binary.LittleEndian.Uint16(raw[3:5])
		offset := int64(value % 128)
		if raw[5]&0x80 != 0 {
			offset += contentChunkSize - 2
		}
		payloadLength := int(raw[5] & 0x3f)
		if raw[5]&0x40 != 0 {
			payloadLength += contentChunkSize - 3
		}
		name := names[int(raw[0])%len(names)]
		to := fuzzLayerPaths[int(raw[2])%len(fuzzLayerPaths)]
		if name == "symlink" {
			to = linkTargets[int(raw[2])%len(linkTargets)]
		}
		var payload []byte
		if name == "write" || name == "splice" {
			payload = fuzzLayerPayload(raw[5], step, payloadLength)
		}
		operations = append(operations, fuzzLayerOperation{
			name:   name,
			from:   fuzzLayerPaths[int(raw[1])%len(fuzzLayerPaths)],
			to:     to,
			offset: offset,
			data:   payload,
			mode:   os.FileMode(value & 0o777),
			mtime:  time.Unix(int64(value), int64(raw[5]&0x3f)).UTC(),
		})
	}
	return operations
}

func fuzzLayerPayload(seed byte, step, length int) []byte {
	payload := make([]byte, length)
	for i := range payload {
		payload[i] = seed + byte(step) + byte(31*i)
	}
	return payload
}

type fuzzLayerOperation struct {
	name   string
	from   string
	to     string
	offset int64
	data   []byte
	mode   os.FileMode
	mtime  time.Time
}

var fuzzLayerPaths = []string{
	"/",
	"/a",
	"/b",
	"/c",
	"/d",
	"/d/a",
	"/d/b",
	"/d/existing",
	"/e",
	"/e/a",
}
