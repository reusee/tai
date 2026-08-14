package taiui

import (
	"bytes"
	"io"
	"time"
)

const TheoryOfKeyInput = `
taiui key input theory:
- ReadKeys decodes raw terminal input into logical key names. The reader
  is expected to be in non-blocking raw mode: a read that returns 0 bytes
  is polled with a short sleep instead of spinning. A lone ESC that never
  grows into a sequence is discarded. ESC followed by a non-sequence byte
  is treated as a stray ESC and the byte is processed normally. Arrow,
  home/end, page-up/page-down, tab, the digit keys 1-3, the bracket keys
  '[' and ']', s, and q (plus Ctrl-C) map to the names "up", "down",
  "home", "end", "pageup", "pagedown", "tab", "1", "2", "3",
  "prev-transition", "next-transition", "split", and "quit". The bracket
  keys name section-transition navigation: a TUI jumps its Output pane to
  the previous or next role or thinking-state transition.
- The function is intentionally transport-agnostic: it accepts an
  io.Reader, so it works with tcell's tty, terminal state files, pipes,
  and test buffers. It does no terminal mode management; the caller
  owns starting and stopping raw mode.
`

// ReadKeys reads raw bytes from r and sends decoded key names to ch.
// It runs until the reader fails. Unknown escape sequences are skipped
// byte-by-byte as needed. See TheoryOfKeyInput.
func ReadKeys(r io.Reader, ch chan<- string) {
	var buf [64]byte
	var pending []byte
	for {
		n, err := r.Read(buf[:])
		if err != nil {
			return
		}
		if n == 0 {
			// The tty is in non-blocking raw mode; avoid a busy loop.
			// An incomplete ESC sequence that never grew is discarded.
			if len(pending) > 0 && pending[0] == 0x1b && len(pending) < 3 {
				pending = pending[:0]
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			if pending[0] == 0x1b {
				if len(pending) == 1 {
					break
				}
				if pending[1] != '[' {
					// ESC followed by a non-sequence byte: the ESC
					// is not part of an escape sequence.
					pending = pending[1:]
					continue
				}
				if len(pending) < 3 {
					break
				}
				switch pending[2] {
				case 'A':
					ch <- "up"
					pending = pending[3:]
					continue
				case 'B':
					ch <- "down"
					pending = pending[3:]
					continue
				case 'H':
					ch <- "home"
					pending = pending[3:]
					continue
				case 'F':
					ch <- "end"
					pending = pending[3:]
					continue
				default:
					if pending[2] >= '0' && pending[2] <= '9' {
						// tilde sequence: ESC [ n ~
						idx := bytes.IndexByte(pending, '~')
						if idx < 0 {
							break
						}
						seq := string(pending[:idx+1])
						switch seq {
						case "\x1b[5~":
							ch <- "pageup"
						case "\x1b[6~":
							ch <- "pagedown"
						case "\x1b[7~":
							ch <- "home"
						case "\x1b[8~":
							ch <- "end"
						}
						pending = pending[idx+1:]
						continue
					}
					// unknown escape sequence: discard one byte
					pending = pending[3:]
					continue
				}
			}
			switch pending[0] {
			case '1':
				ch <- "1"
			case '2':
				ch <- "2"
			case '3':
				ch <- "3"
			case '[':
				ch <- "prev-transition"
			case ']':
				ch <- "next-transition"
			case 's', 'S':
				ch <- "split"
			case 'q', 'Q', 0x03:
				ch <- "quit"
			case '\t':
				ch <- "tab"
			}
			pending = pending[1:]
		}
	}
}
