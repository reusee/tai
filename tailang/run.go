package tailang

func (v *VM) Run(
	yield func(*Interrupt, error) bool,
) {
	for {

		if v.State.IP > len(v.Code) {
			return
		}

		op := v.Code[v.State.IP]
		v.State.IP++

		switch op {

		case OpSuspend:
			yield(InterruptSuspend, nil)
			return

			//TODO other ops

		}

	}
}
