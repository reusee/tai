package changes

import (
	"encoding/xml"
	"fmt"
	"os"
	"time"
)

const TheoryOfErrorLogging = `
When a change block application produces invalid Go, the error is often
discovered by goimports, which reports a formatting-aware parse error that
may obscure the root cause. To surface the real problem earlier and provide
the model with enough context to self-correct, the apply pipeline parses the
modified source immediately after building it — before invoking goimports.
If the parse fails, the error is reported as a parse error rather than a
goimports error, giving the model a clearer signal about what went wrong.

On any error during apply that has meaningful context (source content and/or
modified content), an XML error log is written to the current directory. The
log records the original source file content, the change block (operation,
target, file path, find, body), the modified file content (pre-goimports),
and the error message. The filename follows the pattern
.error-log.<datetime>.xml, using a filesystem-safe timestamp. Same-second
collisions are handled by appending a numeric suffix. This gives the model
the full context needed to analyze why the apply failed and emit a corrected
change block in the next round.
`

type errorLogEntry struct {
	XMLName      xml.Name `xml:"error-log"`
	Timestamp    string   `xml:"timestamp"`
	Operation    string   `xml:"operation"`
	Target       string   `xml:"target"`
	FilePath     string   `xml:"file-path"`
	Find         string   `xml:"find,omitempty"`
	ChangeBlock  string   `xml:"change-block"`
	SourceFile   string   `xml:"source-file"`
	ModifiedFile string   `xml:"modified-file"`
	Error        string   `xml:"error"`
}

// writeErrorLog writes an XML error log to the current directory, recording
// the original source, the change block, the modified content, and the error.
// The filename follows the pattern .error-log.<datetime>.xml, using a
// filesystem-safe timestamp. Same-second collisions are handled by appending
// a numeric suffix. See TheoryOfErrorLogging.
func writeErrorLog(h ChangeBlock, sourceFile []byte, modifiedFile []byte, applyErr error) error {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf(".error-log.%s.xml", timestamp)

	// Handle same-second collisions by appending a numeric suffix.
	for i := 0; ; i++ {
		candidate := filename
		if i > 0 {
			candidate = fmt.Sprintf(".error-log.%s-%d.xml", timestamp, i)
		}
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			filename = candidate
			break
		}
		if i > 999 {
			break // avoid infinite loop; overwrite the base name
		}
	}

	entry := errorLogEntry{
		Timestamp:    time.Now().Format(time.RFC3339),
		Operation:    h.Op,
		Target:       h.Target,
		FilePath:     h.FilePath,
		Find:         h.Find,
		ChangeBlock:  h.Body,
		SourceFile:   string(sourceFile),
		ModifiedFile: string(modifiedFile),
		Error:        applyErr.Error(),
	}

	data, err := xml.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, append(data, '\n'), 0644)
}
