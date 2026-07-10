package cmds

import (
	"fmt"
	"maps"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/reusee/tai/vars"
)

type Executor struct {
	commands map[string]*Command
}

func NewExecutor() *Executor {
	ret := &Executor{
		commands: make(map[string]*Command),
	}

	usage := Func(func() {
		ret.PrintUsage()
		os.Exit(0)
	}).
		Desc("print this usage").
		Alias("help", "-help", "--help")
	ret.Define("-h", usage)

	return ret
}

func (p *Executor) Define(name string, command *Command) {
	if _, ok := p.commands[name]; ok {
		panic(fmt.Errorf("duplicated command %s", name))
	}
	p.commands[name] = command
	for _, name := range command.Aliases {
		if _, ok := p.commands[name]; ok {
			panic(fmt.Errorf("duplicated command %s", name))
		}
		p.commands[name] = command
	}
}

var errorType = reflect.TypeFor[error]()

func (p *Executor) Execute(args []string) error {
	commands := p.commands
	for {
		if len(args) == 0 {
			return nil
		}

		name := strings.TrimSpace(args[0])
		args = args[1:]

		command, ok := commands[name]
		if !ok {
			return fmt.Errorf("unknown command: %s", name)
		}

		if command.Func.IsValid() {
			var callArgs []reflect.Value
			for i, max := 0, command.Func.Type().NumIn(); i < max; i++ {
				value, err := getArg(command.Func.Type().In(i), args)
				if err != nil {
					return err
				}
				if len(args) > 0 {
					args = args[1:]
				}
				callArgs = append(callArgs, value)
			}
			rets := command.Func.Call(callArgs)
			if len(rets) > 0 {
				err := rets[0].Interface().(error)
				if err != nil {
					return err
				}
			}
		}

		if len(command.Subs) > 0 {
			commands = maps.Clone(commands)
			for subname, cmd := range command.Subs {
				if _, ok := commands[subname]; ok {
					return fmt.Errorf("duplicated sub command: %s %s", name, subname)
				}
				commands[subname] = cmd
			}
		}

	}
}

func (p *Executor) MustExecute(args []string) {
	if err := p.Execute(args); err != nil {
		panic(err)
	}
}

// TheoryOfCommandExecution captures the design rationale of the command
// executor. The Executor evaluates commands in a loop, treating
// sub-commands as additive extensions to the current command namespace.
// Parameters of a command function are bound positionally to remaining
// arguments through reflective parsing; a parameter of pointer kind is
// optional and takes a zero value when no argument is supplied. Numeric
// parsing must respect the declared parameter's bit width so that
// out-of-range inputs are rejected rather than silently truncated.
const TheoryOfCommandExecution = `
The Executor evaluates commands in a loop, treating sub-commands as additive
extensions to the current command namespace. Parameters of a command function
are bound positionally to remaining arguments through reflective parsing. A
parameter of pointer kind is optional and takes a zero value when no argument
is supplied. Numeric parsing must respect the declared parameter's bit width
so out-of-range inputs are rejected rather than silently truncated.
`

func getArg(t reflect.Type, args []string) (ret reflect.Value, err error) {
	if len(args) == 0 {

		if t.Kind() == reflect.Pointer {
			// optional, use zero value
			return reflect.New(t.Elem()), nil
		}

		return ret, fmt.Errorf("expecting argument, got nothing")
	}

	if t.Kind() == reflect.Pointer {
		elemValue, err := getArg(t.Elem(), args)
		if err != nil {
			return ret, err
		}
		ret = elemValue.Addr()
		return ret, nil
	}

	str := args[0]

	ret = reflect.New(t).Elem()

	switch t.Kind() {

	case reflect.Bool:
		v := vars.StrToBool(str)
		ret.SetBool(v)
		return

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Honor the declared parameter's bit width so out-of-range inputs
		// are reported as errors instead of being silently truncated by
		// reflect.Value.SetInt. t.Bits() returns 8/16/32/64 as appropriate.
		v, err := strconv.ParseInt(str, 10, t.Bits())
		if err != nil {
			return ret, fmt.Errorf("convert %s to int: %w", str, err)
		}
		ret.SetInt(v)
		return ret, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(str, 10, t.Bits())
		if err != nil {
			return ret, fmt.Errorf("convert %s to unsigned int: %w", str, err)
		}
		ret.SetUint(v)
		return ret, nil

	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(str, t.Bits())
		if err != nil {
			return ret, fmt.Errorf("convert %s to float: %w", str, err)
		}
		ret.SetFloat(v)
		return ret, nil

	case reflect.String:
		ret.SetString(str)
		return

	}

	return ret, fmt.Errorf("unsupported type: %v", t)
}
