package gocodes

import (
	"strings"
	"testing"
)

func TestWithModModEnv(t *testing.T) {
	t.Run("no existing GOFLAGS", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "HOME=/root"}
		result := withModModEnv(envs)
		found := false
		for _, e := range result {
			if e == "GOFLAGS=-mod=mod" {
				found = true
			}
		}
		if !found {
			t.Fatal("GOFLAGS=-mod=mod not added")
		}
		if len(result) != len(envs)+1 {
			t.Fatalf("expected %d entries, got %d", len(envs)+1, len(result))
		}
	})

	t.Run("existing GOFLAGS without mod", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "GOFLAGS=-trimpath"}
		result := withModModEnv(envs)
		found := false
		for _, e := range result {
			if e == "GOFLAGS=-trimpath -mod=mod" {
				found = true
			}
		}
		if !found {
			t.Fatalf("GOFLAGS not merged correctly, got %v", result)
		}
		if len(result) != len(envs) {
			t.Fatalf("expected same length, got %d", len(result))
		}
	})

	t.Run("existing GOFLAGS with mod=mod", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=mod"}
		result := withModModEnv(envs)
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				if strings.Count(e, "-mod=mod") > 1 {
					t.Fatalf("GOFLAGS should contain -mod=mod only once, got %s", e)
				}
			}
		}
		if len(result) != len(envs) {
			t.Fatalf("expected same length, got %d", len(result))
		}
	})

	t.Run("existing GOFLAGS with mod=vendor not overridden", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=vendor"}
		result := withModModEnv(envs)
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				if strings.Contains(e, "-mod=mod") {
					t.Fatal("should not override -mod=vendor with -mod=mod")
				}
			}
		}
		if len(result) != len(envs) {
			t.Fatalf("expected same length, got %d", len(result))
		}
	})

	t.Run("does not modify original slice", func(t *testing.T) {
		envs := []string{"GOFLAGS=-trimpath"}
		_ = withModModEnv(envs)
		if envs[0] != "GOFLAGS=-trimpath" {
			t.Fatalf("original slice was modified: %v", envs)
		}
	})
}
