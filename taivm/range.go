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

func (r *Range) Len() int64 {
	if r.Step > 0 {
		if r.Start >= r.Stop {
			return 0
		}
		return (r.Stop - r.Start + r.Step - 1) / r.Step
	} else if r.Step < 0 {
		if r.Start <= r.Stop {
			return 0
		}
		return (r.Start - r.Stop - r.Step - 1) / -r.Step
	}
	return 0
}
