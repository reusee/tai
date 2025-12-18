package taivm

type ListIterator struct {
	List []any
	Idx  int
}

type MapIterator struct {
	Keys []any
	Idx  int
}
