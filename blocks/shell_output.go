package blocks

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// TheoryOfShellOutputCapture documents the bounded capture of shell
// command output and the spill file behind it.
const TheoryOfShellOutputCapture = `
Command output is bounded: a stream's in-context excerpt is capped by
maxShellOutputBytes. Within the cap the stream is kept whole; beyond it
the head keeps the first maxShellOutputBytes, a note names the spill file
and the omitted byte count, and the tail keeps the last
shellOutputTailBytes. The head shows what the command did; the tail shows
where it ended, and the end of a command's output is where failures
appear. The spill file is the whole output, readable with an ingest
block, so the model keeps access to every byte and no summary stands
between it and the output.

Bounding follows the project's context strategy: the excerpt is the
index, the spill file is the fetch. It applies to every command,
foreground and background alike, so no single command can flood the
context or the session's memory, and a runaway output cannot stall the
round that waits for it.
`

const (
	// maxShellOutputBytes caps the output one stream keeps in context.
	maxShellOutputBytes = 16 << 10
	// shellOutputTailBytes is the tail excerpt kept after a spill: the end
	// of a command's output is where failures appear.
	shellOutputTailBytes = 2 << 10
)

// boundedCapture is an io.Writer that keeps a bounded excerpt of one
// command stream and spills the full content into a temporary file once
// the excerpt cap is exceeded. See TheoryOfShellOutputCapture.
type boundedCapture struct {
	limit int

	// head holds the first limit bytes of a stream that stayed within the
	// cap, and the head excerpt of a spilled one. It never grows after
	// the spill.
	head bytes.Buffer

	total int
	// tail holds the last shellOutputTailBytes of a spilled stream.
	tail []byte

	spilled  bool
	file     *os.File
	path     string
	spillErr error
}

// newBoundedCapture creates a capture that keeps at most limit bytes of
// head in context.
func newBoundedCapture(limit int) *boundedCapture {
	return &boundedCapture{limit: limit}
}

// Write appends p to the capture. A stream within the cap is buffered
// whole; a stream that crosses the cap fills the head excerpt to the cap,
// spills its full content into a temporary file, and keeps only the head
// and tail excerpts in memory, so the capture stays bounded even when the
// command produces gigabytes.
func (c *boundedCapture) Write(p []byte) (int, error) {
	n := len(p)
	c.total += n
	if !c.spilled {
		if c.head.Len()+n <= c.limit {
			c.head.Write(p)
			return n, nil
		}
		if room := c.limit - c.head.Len(); room > 0 {
			c.head.Write(p[:room])
			p = p[room:]
		}
		c.spilled = true
		c.spill()
	}
	if c.file != nil {
		if _, err := c.file.Write(p); err != nil && c.spillErr == nil {
			c.spillErr = err
		}
	}
	c.appendTail(p)
	return n, nil
}

// spill opens the temporary file and writes the head buffered so far into
// it, so the file holds the stream from its first byte. A spill failure is
// recorded and reported in the excerpt: the capture keeps its bounded
// excerpt instead of failing the command.
func (c *boundedCapture) spill() {
	file, err := os.CreateTemp("", "tai-shell-*.log")
	if err != nil {
		c.spillErr = err
		return
	}
	if _, err := file.Write(c.head.Bytes()); err != nil {
		c.spillErr = err
		file.Close()
		return
	}
	c.file = file
	c.path = file.Name()
}

// appendTail keeps the last shellOutputTailBytes of the stream.
func (c *boundedCapture) appendTail(p []byte) {
	if len(p) >= shellOutputTailBytes {
		c.tail = append(c.tail[:0], p[len(p)-shellOutputTailBytes:]...)
		return
	}
	c.tail = append(c.tail, p...)
	if len(c.tail) > shellOutputTailBytes {
		c.tail = append(c.tail[:0], c.tail[len(c.tail)-shellOutputTailBytes:]...)
	}
}

// Close closes the spill file, keeping its content on disk: the excerpt
// names the path and a later ingest block reads the file.
func (c *boundedCapture) Close() {
	if c.file != nil {
		c.file.Close()
	}
}

// String renders the capture for the model: the whole stream within the
// cap, or the head excerpt, a note naming the spill file and the omitted
// byte count, and the tail excerpt.
func (c *boundedCapture) String() string {
	if !c.spilled {
		return c.head.String()
	}
	var sb strings.Builder
	sb.Write(c.head.Bytes())
	if c.path != "" {
		fmt.Fprintf(&sb, "\n... [%d bytes omitted; full output at %s] ...\n", c.omittedBytes(), c.path)
	} else {
		fmt.Fprintf(&sb, "\n... [%d bytes omitted; the full output could not be saved: %v] ...\n", c.omittedBytes(), c.spillErr)
	}
	sb.Write(c.tail)
	return sb.String()
}

// omittedBytes returns the number of stream bytes the excerpt does not
// carry.
func (c *boundedCapture) omittedBytes() int {
	omitted := c.total - c.head.Len() - len(c.tail)
	if omitted < 0 {
		return 0
	}
	return omitted
}
