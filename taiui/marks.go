package taiui

import "sync"

// marksPool pools the fill-tracking marks slices. Fill paths allocate
// one marks slice per filled element per render; pooling avoids the
// allocation for the common screen-sized boxes.
var marksPool = sync.Pool{
	New: func() any { return make([]bool, 0, 80*24) },
}

// getMarks returns a cleared marks slice of the given size, from the
// pool when possible.
func getMarks(size int) []bool {
	marks := marksPool.Get().([]bool)
	if cap(marks) < size {
		marks = make([]bool, size)
	}
	marks = marks[:size]
	clear(marks)
	return marks
}

// putMarks returns a marks slice to the pool.
func putMarks(marks []bool) {
	marksPool.Put(marks)
}
