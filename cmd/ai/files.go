package main

import (
	"os"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/generators"
)

func filePathToPart(path string) (_ generators.Part, err error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
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
		return generators.Text(
			content,
		), nil
	}

	return generators.FileContent{
		Content:  content,
		MimeType: mtype.String(),
	}, nil
}
