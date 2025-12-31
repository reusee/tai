package configs

import "reflect"

type Configurable interface {
	ConfigExpr() string
}

var configurableType = reflect.TypeFor[Configurable]()
