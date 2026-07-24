package flags

// Flag is the interface for command-line flag types. Each Flag type
// registers its keys and descriptions via Keys, and handles argument
// consumption via Handle.
type Flag interface {
	// Keys returns a map from each flag key to its human-readable
	// description. The description is displayed in usage output. Each
	// key must be unique across all Flag types in the scope; Parse
	// returns an error on duplicate key registrations.
	Keys() map[string]string
	Handle(key string, args []string) (newValue any, remainArgs []string, err error)
}
