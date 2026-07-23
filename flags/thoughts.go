package flags

// Thoughts wraps an optional boolean. A nil Value means the flag was never
// set; a non-nil Value points to the resolved true/false state.
type Thoughts struct {
	Value *bool
}

func (Module) Thoughts() (ret Thoughts) {
	return
}

var _ Flag = Thoughts{}

func (t Thoughts) Keys() []string {
	return []string{"-thoughts", "-no-thoughts"}
}

func (t Thoughts) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	// The matched key determines the boolean value; "thoughts" sets true,
	// "no-thoughts" sets false. A fresh *bool is allocated so each
	// invocation produces an independent pointer.
	value := key == "-thoughts"
	newValue = Thoughts{Value: &value}
	remainArgs = args
	return
}
