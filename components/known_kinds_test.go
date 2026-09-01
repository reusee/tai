package components

import "testing"

// TestComponentSetKnownKinds verifies the derivation of the session's
// processable block kinds: every component that declares a Kind is
// known — processable or prompt-only — plus the caller's extra kinds,
// and nothing else, including the kindless empty kind.
// See pipeline.TheoryOfUnknownBlockKinds.
func TestComponentSetKnownKinds(t *testing.T) {
	comps := ComponentSet{
		{Kind: "shell"},
		{PromptSection: "prompt-only section"},
		{Kind: "memory", PromptSection: "memory prompt"},
	}
	known := comps.KnownKinds("done")
	for _, kind := range []string{"shell", "memory", "done"} {
		if !known(kind) {
			t.Fatalf("expected kind %q to be known", kind)
		}
	}
	for _, kind := range []string{"mystery", "change", ""} {
		if known(kind) {
			t.Fatalf("expected kind %q to be unknown", kind)
		}
	}
}
