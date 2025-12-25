package taigo

import "io"

type Options struct {
	Stdin   io.Reader      // if nil, default to os.Stdin
	Stdout  io.Writer      // if nil, default to os.Stdout
	Stderr  io.Writer      // if nil, default to os.Stderr
	Modules map[string]any // map of path to module object (map or struct)
}
