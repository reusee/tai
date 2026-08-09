package taiui

import (
	"os"
	"strings"

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
- Width options are derived from the environment on every measurement.
  There are no caches: an environment change takes effect immediately,
  and a measurement is a table walk.
`

// displayWidthOptions returns the display-width options derived from the
// user's environment, mirroring the tcell RUNEWIDTH_EASTASIAN toggle.
func displayWidthOptions() displaywidth.Options {
	if rw := strings.ToLower(os.Getenv("RUNEWIDTH_EASTASIAN")); rw == "1" || rw == "true" || rw == "yes" {
		return displaywidth.Options{EastAsianWidth: true}
	}
	return displaywidth.Options{}
}
