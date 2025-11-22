package main

import (
	"os"
	"path/filepath"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
)

var files []string

func init() {
	cmds.Define("-file", cmds.Func(func(pattern string) {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			// ignore
			files = append(files, pattern)
		} else {
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				if info.IsDir() {
					continue
				}
				files = append(files, path)
			}
		}
	}).Desc("add matching files to context"))
}

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
