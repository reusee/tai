package debugs

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const TheoryOfErrorLogging = `
When a change block application produces invalid Go, the error is often
discovered by goimports, which reports a formatting-aware parse error that
may obscure the root cause. To surface the real problem earlier, the apply
pipeline parses the modified source immediately after building it — before
invoking goimports. If the parse fails, the error is reported as a parse
error rather than a goimports error.

On any error during apply that has meaningful context (source content and/or
modified content), an XML error log is written to the error log directory. The
log records the original source file content, the change block (operation,
target, file path, find, body), the modified file content (pre-goimports),
and the error message. The filename follows the pattern
.error-log.<datetime>.xml, using a filesystem-safe timestamp. Same-second
collisions are handled by appending a numeric suffix.

ErrorLogDir is a dscope-provided type that controls where error logs are
written. The default is the current working directory. In test environments,
the provider is overridden with a temporary directory.
`

// ErrorLogDir controls where error log XML files are written. The default
// provider returns the current working directory. Tests override it with
// a temporary directory to avoid polluting the working directory.
// See TheoryOfErrorLogging.
type ErrorLogDir string

func (Module) ErrorLogDir() ErrorLogDir {
	dir, err := os.Getwd()
	if err != nil {
		return ErrorLogDir(".")
	}
	return ErrorLogDir(dir)
}

// ErrorLogContext carries the structured context for a single error log
// entry. The caller (typically in the changes package) flattens change
// block fields and source/modified content into this struct so that the
// debugs package does not depend on the changes package.
type ErrorLogContext struct {
	Operation    string
	Target       string
	FilePath     string
	Find         string
	ChangeBlock  string
	SourceFile   string
	ModifiedFile string
	Error        string
}

// WriteErrorLog writes a structured XML error log entry to the error log
// directory. It is a dscope-provided function type so the directory is
// injected via ErrorLogDir without mutable global state.
// See TheoryOfErrorLogging.
type WriteErrorLog func(ctx ErrorLogContext) error

func (Module) WriteErrorLog(dir ErrorLogDir) WriteErrorLog {
	return func(ctx ErrorLogContext) error {
		timestamp := time.Now().Format("20060102-150405")
		base := fmt.Sprintf(".error-log.%s", timestamp)

		var filename string
		for i := 0; ; i++ {
			candidate := base
			if i > 0 {
				candidate = fmt.Sprintf("%s-%d", base, i)
			}
			candidate += ".xml"
			fullPath := filepath.Join(string(dir), candidate)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				filename = fullPath
				break
			}
			if i > 999 {
				filename = filepath.Join(string(dir), base+".xml")
				break
			}
		}

		entry := errorLogEntry{
			Timestamp:    time.Now().Format(time.RFC3339),
			Operation:    ctx.Operation,
			Target:       ctx.Target,
			FilePath:     ctx.FilePath,
			Find:         ctx.Find,
			ChangeBlock:  ctx.ChangeBlock,
			SourceFile:   ctx.SourceFile,
			ModifiedFile: ctx.ModifiedFile,
			Error:        ctx.Error,
		}

		data, err := xml.MarshalIndent(entry, "", "  ")
		if err != nil {
			return err
		}

		return os.WriteFile(filename, append(data, '\n'), 0644)
	}
}

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
