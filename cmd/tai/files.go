package main

import (
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
between reference material and the task request. Files outside writable
directories are marked with a "(read-only)" annotation in the begin marker,
informing the model that these files are for reference only and must not be
modified.
`

func filePathToParts(path string) ([]generators.Part, error) {
	// Files outside writable directories are marked as read-only rather
	// than rejected, because the model can still use their content as
	// reference even though it cannot modify them. The "(read-only)"
	// annotation in the begin marker informs the model not to emit change
	// blocks targeting these files.
	// See security.TheoryOfWritableDirs and TheoryOfFocusFileDirectoryCheck
	// in anytexts/code_provider.go.
	outside, err := pathutil.IsOutsideWritableDirs(path)
	if err != nil {
		return nil, err
	}
	readOnlyNote := ""
	if outside {
		readOnlyNote = " (read-only)"
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
		text := "``` begin of file " + path + readOnlyNote + "\n" +
			string(content) + "\n" +
			"``` end of file " + path + "\n"
		return []generators.Part{generators.Text(text)}, nil
	}

	return []generators.Part{
		generators.Text("``` begin of file " + path + " (binary, " + mtype.String() + ")" + readOnlyNote + "\n"),
		generators.FileContent{
			Content:  content,
			MimeType: mtype.String(),
		},
		generators.Text("\n``` end of file " + path + "\n"),
	}, nil
}
