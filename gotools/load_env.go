package gotools

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
In workspace mode the go command forbids -mod=mod ("-mod may only be set to
readonly or vendor when in workspace mode"), so the injection is skipped and
any incompatible -mod= flag is stripped from GOFLAGS instead; -mod=readonly
and -mod=vendor are preserved. See TheoryOfWorkspace.
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

// withoutModModEnv returns a copy of envs with any -mod= flag other than
// readonly or vendor removed from GOFLAGS. Workspace mode forbids -mod=mod;
// stripping it lets workspace loading proceed with the workspace default
// (readonly). See TheoryOfModModEnv and TheoryOfWorkspace.
func withoutModModEnv(envs []string) []string {
	ret := slices.Clone(envs)
	for i, e := range ret {
		if !strings.HasPrefix(e, "GOFLAGS=") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(e, "GOFLAGS="))
		kept := make([]string, 0, len(fields))
		for _, f := range fields {
			if strings.HasPrefix(f, "-mod=") {
				value := strings.TrimPrefix(f, "-mod=")
				if value == "readonly" || value == "vendor" {
					kept = append(kept, f)
				}
				continue
			}
			kept = append(kept, f)
		}
		if len(kept) == 0 {
			ret = append(ret[:i], ret[i+1:]...)
		} else {
			ret[i] = "GOFLAGS=" + strings.Join(kept, " ")
		}
		break
	}
	return ret
}

// withReadonlyModEnv returns a copy of envs with GOFLAGS set to include
// -mod=readonly, replacing any existing -mod= flag. gopls uses this
// normalization so its module loading never rewrites go.mod or go.sum,
// even when the loader injected -mod=mod into the environment.
// See TheoryOfGopls.
func withReadonlyModEnv(envs []string) []string {
	var goflags string
	found := false
	out := make([]string, 0, len(envs)+1)
	for _, e := range envs {
		if strings.HasPrefix(e, "GOFLAGS=") {
			goflags = e
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return append(out, "GOFLAGS=-mod=readonly")
	}
	fields := strings.Fields(strings.TrimPrefix(goflags, "GOFLAGS="))
	kept := make([]string, 0, len(fields)+1)
	sawMod := false
	for _, f := range fields {
		if strings.HasPrefix(f, "-mod=") {
			if !sawMod {
				kept = append(kept, "-mod=readonly")
				sawMod = true
			}
			continue
		}
		kept = append(kept, f)
	}
	if !sawMod {
		kept = append(kept, "-mod=readonly")
	}
	out = append(out, "GOFLAGS="+strings.Join(kept, " "))
	return out
}
