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

func (it *RangeIterator) GetIndex(key any) (any, bool) {
	return key, true
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

func (r *Range) Contains(val int64) bool {
	if r.Step > 0 {
		return val >= r.Start && val < r.Stop && (val-r.Start)%r.Step == 0
	}
	return val <= r.Start && val > r.Stop && (r.Start-val)%(-r.Step) == 0
}
