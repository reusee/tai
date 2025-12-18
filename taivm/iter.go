package taivm

type ListIterator struct {
	List *List
	Idx  int
}

type MapIterator struct {
	Keys []any
	Idx  int
}
