package taiui

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
)

const TheoryOfDisplayWidth = `
taiui display width theory:
- Display width is a property of a grapheme cluster, not of a single
  rune: a combining sequence such as e + combining acute is one cluster
  of width 1, and a ZWJ emoji sequence is one cluster spanning several
  columns. Text rendering segments lines into clusters and measures each
  cluster once.
- The RUNEWIDTH_EASTASIAN environment variable toggles the width of
  ambiguous East Asian runes: 1 when unset, 2 when set to 1, true, or
  yes. This preserves the historical tcell convention for CJK terminals.
- DisplayWidthOptions derives the options from the environment. Render
  calls it once per pass and threads the options through the element
  tree, so the environment is scanned once per pass rather than once per
  element. Screens that measure clusters independently (such as the demo
  presenter) call it per present to stay in sync with the renderer. There
  are no caches: an environment change takes effect on the next call,
  and a measurement is a table walk.
`

// DisplayWidthOptions returns the display-width options derived from the
// user's environment, mirroring the tcell RUNEWIDTH_EASTASIAN toggle.
// Render derives the options once per pass and threads them through the
// element tree, so the environment is scanned once per frame rather than
// once per element. Screens that measure clusters independently (such as
// the demo presenter) call it per present to stay in sync with the
// renderer.
func DisplayWidthOptions() displaywidth.Options {
	if rw := strings.ToLower(os.Getenv("RUNEWIDTH_EASTASIAN")); rw == "1" || rw == "true" || rw == "yes" {
		return displaywidth.Options{EastAsianWidth: true}
	}
	return displaywidth.Options{}
}

// clusterWidth returns the display width of the grapheme cluster formed by
// mainc and its combining runes. Combining runes can change the width (an
// emoji variation selector widens its base), so the whole cluster is
// measured. The cluster bytes are built with utf8.AppendRune into a stack
// buffer: short clusters (a base rune with one or two combining runes)
// allocate nothing on the heap, and a longer cluster spills into a fresh
// buffer.
func clusterWidth(options displaywidth.Options, mainc rune, combc []rune) int {
	if len(combc) == 0 {
		return options.Rune(mainc)
	}
	var buf [8]byte
	b := buf[:0]
	b = utf8.AppendRune(b, mainc)
	for _, r := range combc {
		b = utf8.AppendRune(b, r)
	}
	return options.Bytes(b)
}
