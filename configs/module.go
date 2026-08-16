package configs

import "github.com/reusee/dscope"

type Module struct {
	dscope.Module
}

// DefaultLoader returns a Loader with no configuration files, schema, or
// globals. It is the default provider for configs.Loader in scopes that
// embed configs.Module; specialized loaders (e.g., taiconfigs) override it
// via Fork.
func (Module) DefaultLoader() Loader {
	return NewLoader(nil, LoaderConfig{})
}
