package gocodes

import (
	"slices"
	"strings"
)

const TheoryOfModModEnv = `
GOFLAGS=-mod=mod is injected into the load environment so go list can update
go.mod automatically if it is out of sync, rather than failing with "updates
to go.mod needed". The helper preserves any existing GOFLAGS value, appending
-mod=mod only when no -mod= flag is already present, to avoid conflicting with
-mod=vendor or other explicit module modes.
`

// withModModEnv returns a copy of envs with -mod=mod added to GOFLAGS.
// If GOFLAGS is not set, it is added as a new entry. If GOFLAGS is already
// set and does not contain a -mod= flag, -mod=mod is appended. If GOFLAGS
// already contains a -mod= flag (e.g., -mod=vendor), the env is returned
// unchanged to avoid conflicting module mode settings.
// See TheoryOfModModEnv.
func withModModEnv(envs []string) []string {
	ret := slices.Clone(envs)
	for i, e := range ret {
		if strings.HasPrefix(e, "GOFLAGS=") {
			if !strings.Contains(e, "-mod=") {
				ret[i] = e + " -mod=mod"
			}
			return ret
		}
	}
	return append(ret, "GOFLAGS=-mod=mod")
}
