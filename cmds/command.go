package cmds

import (
	"fmt"
	"reflect"
)

type Command struct {
	Func        reflect.Value
	Subs        map[string]*Command
	Description string
	Aliases     []string
}

func (c *Command) Desc(desc string) *Command {
	c.Description = desc
	return c
}

func (c *Command) Alias(names ...string) *Command {
	c.Aliases = append(c.Aliases, names...)
	return c
}

func Func(fn any) *Command {
	fnValue := reflect.ValueOf(fn)

	if fnValue.Kind() != reflect.Func {
		panic(fmt.Errorf("must be function, got %T", fn))
	}

	numRets := fnValue.Type().NumOut()
	if numRets >= 2 {
		panic(fmt.Errorf("must return 0 or 1 value"))
	}
	if numRets == 1 && fnValue.Type().Out(0) != errorType {
		panic(fmt.Errorf("must return error"))
	}

	command := &Command{
		Func: fnValue,
	}

	return command
}

func Sub(subs map[string]*Command) *Command {
	return &Command{
		Subs: subs,
	}
}
