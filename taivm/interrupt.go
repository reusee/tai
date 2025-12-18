package taivm

type Interrupt struct {
	Suspend bool
}

var (
	InterruptSuspend = &Interrupt{
		Suspend: true,
	}
)
