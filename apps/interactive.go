package apps

// Interactive reports whether an app reads multi-turn interactive
// input while running. A display frontend renders its input bar only
// for interactive apps. An app declares itself interactive by forking
// Interactive(true) into its Defs; the Module default is false.
// See TheoryOfApps.
type Interactive bool

// Interactive provides the default: not interactive.
func (Module) Interactive() Interactive {
	return false
}
