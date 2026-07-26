package generators

import "github.com/reusee/tai/flags"

// TemperatureFlag is an alias for flags.TemperatureFlag, controlled by the
// -temperature flag. When non-nil, it overrides the spec's Temperature
// setting.
type TemperatureFlag = flags.TemperatureFlag
