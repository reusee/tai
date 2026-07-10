package main

import (
	"os"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/generators"
)

const TheoryOfContextStructure = `
Context provided to the model must clearly delineate each file's boundaries using
begin/end markers that include the file path. Without markers, the model cannot
distinguish where one file ends and another begins, especially when file content
contains code fences or similar delimiters. Binary files must also be wrapped with
markers so the model understands the attachment boundary. User input must be
separated from file context with its own marker so the model can distinguish
between reference material and the task request.
`

func filePathToParts(path string) ([]generators.Part, error) {
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
