package configs

import "reflect"

type Configurable interface {
	TaigoConfigurable()
}

var configurableType = reflect.TypeFor[Configurable]()
