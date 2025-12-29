package taivm

type Interrupt struct {
	Suspend bool
	Yield   bool
}

var (
	InterruptSuspend = &Interrupt{
		Suspend: true,
	}
	InterruptYield = &Interrupt{
		Yield: true,
	}
)
