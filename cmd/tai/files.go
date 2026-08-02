package main

import (
	"fmt"
	"os"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/pathutil"
)

const TheoryOfContextStructure = `
Context provided to the model must clearly delineate each file's boundaries using
begin/end markers that include the file path. Binary files must also be wrapped with
markers so the model understands the attachment boundary. User input must be
separated from file context with its own marker so the model can distinguish
between reference material and the task request.
`

func filePathToParts(path string) ([]generators.Part, error) {
	// Reject focus files outside all writable directories at collection
	// time rather than at apply time. The writable directories match
	// the security package's container filesystem policy: CWD, Go
	// toolchain dirs, config dir, /tmp, and /dev/shm. See
	// security.TheoryOfWritableDirs and TheoryOfFocusFileDirectoryCheck
	// in anytexts/code_provider.go.
	outside, err := pathutil.IsOutsideWritableDirs(path)
	if err != nil {
		return nil, err
	}
	if outside {
		return nil, fmt.Errorf("focus file outside writable directory: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	mtype := mimetype.Detect(content)
	isText := false
	for t := mtype; t != nil; t = t.Parent() {
		if t.Is("text/plain") {
			isText = true
			break
		}
	}

	if isText {
		text := "``` begin of file " + path + "\n" +
			string(content) + "\n" +
			"``` end of file " + path + "\n"
		return []generators.Part{generators.Text(text)}, nil
	}

	return []generators.Part{
		generators.Text("``` begin of file " + path + " (binary, " + mtype.String() + ")\n"),
		generators.FileContent{
			Content:  content,
			MimeType: mtype.String(),
		},
		generators.Text("\n``` end of file " + path + "\n"),
	}, nil
}
