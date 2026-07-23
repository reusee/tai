package flags

type Shell bool

func (Module) Shell() (ret Shell) {
	return
}

var _ Flag = Shell(false)

func (s Shell) Keys() []string {
	return []string{"-shell", "-no-shell"}
}

func (s Shell) Handle(key string, args []string) (newValue any, remainArgs []string, err error) {
	// The matched key determines the boolean value; "shell" sets true,
	// "no-shell" sets false. No arguments are consumed.
	newValue = Shell(key == "-shell")
	remainArgs = args
	return
}
