package tailang

type Interrupt struct {
	Suspend bool
}

var (
	InterruptSuspend = &Interrupt{
		Suspend: true,
	}
)
