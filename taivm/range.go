package taivm

type Range struct {
	Start int64
	Stop  int64
	Step  int64
}

type RangeIterator struct {
	Range *Range
	Curr  int64
}
