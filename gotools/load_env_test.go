package gotools

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

func TestWithoutModModEnv(t *testing.T) {
	t.Run("removes mod=mod", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "GOFLAGS=-trimpath -mod=mod"}
		result := withoutModModEnv(envs)
		found := false
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				found = true
				if strings.Contains(e, "-mod=") {
					t.Fatalf("-mod flag not removed, got %q", e)
				}
				if e != "GOFLAGS=-trimpath" {
					t.Fatalf("GOFLAGS not preserved, got %q", e)
				}
			}
		}
		if !found {
			t.Fatalf("GOFLAGS entry missing, got %v", result)
		}
	})

	t.Run("keeps mod=readonly", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=readonly"}
		result := withoutModModEnv(envs)
		found := false
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				found = true
				if !strings.Contains(e, "-mod=readonly") {
					t.Fatalf("-mod=readonly should be preserved, got %q", e)
				}
			}
		}
		if !found {
			t.Fatalf("GOFLAGS entry missing, got %v", result)
		}
	})

	t.Run("keeps mod=vendor", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=vendor"}
		result := withoutModModEnv(envs)
		found := false
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				found = true
				if !strings.Contains(e, "-mod=vendor") {
					t.Fatalf("-mod=vendor should be preserved, got %q", e)
				}
			}
		}
		if !found {
			t.Fatalf("GOFLAGS entry missing, got %v", result)
		}
	})

	t.Run("removes GOFLAGS when empty", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "GOFLAGS=-mod=mod"}
		result := withoutModModEnv(envs)
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				t.Fatalf("empty GOFLAGS should be removed, got %v", result)
			}
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 entry, got %v", result)
		}
	})

	t.Run("no GOFLAGS unchanged", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "HOME=/root"}
		result := withoutModModEnv(envs)
		if len(result) != len(envs) {
			t.Fatalf("expected same length, got %v", result)
		}
	})

	t.Run("does not modify original slice", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=mod"}
		_ = withoutModModEnv(envs)
		if envs[0] != "GOFLAGS=-mod=mod" {
			t.Fatalf("original slice was modified: %v", envs)
		}
	})
}

func TestWithReadonlyModEnv(t *testing.T) {
	t.Run("no existing GOFLAGS", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "HOME=/root"}
		result := withReadonlyModEnv(envs)
		found := false
		for _, e := range result {
			if e == "GOFLAGS=-mod=readonly" {
				found = true
			}
		}
		if !found {
			t.Fatal("GOFLAGS=-mod=readonly not added")
		}
		if len(result) != len(envs)+1 {
			t.Fatalf("expected %d entries, got %d", len(envs)+1, len(result))
		}
	})

	t.Run("existing GOFLAGS without mod", func(t *testing.T) {
		envs := []string{"PATH=/usr/bin", "GOFLAGS=-trimpath"}
		result := withReadonlyModEnv(envs)
		found := false
		for _, e := range result {
			if e == "GOFLAGS=-trimpath -mod=readonly" {
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

	t.Run("existing GOFLAGS with mod=mod replaced", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=mod"}
		result := withReadonlyModEnv(envs)
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				if e != "GOFLAGS=-mod=readonly" {
					t.Fatalf("expected -mod=readonly, got %q", e)
				}
			}
		}
	})

	t.Run("existing GOFLAGS with mod=vendor replaced", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=vendor"}
		result := withReadonlyModEnv(envs)
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				if strings.Contains(e, "-mod=vendor") {
					t.Fatal("-mod=vendor should be replaced")
				}
				if !strings.Contains(e, "-mod=readonly") {
					t.Fatalf("-mod=readonly missing, got %q", e)
				}
			}
		}
	})

	t.Run("last GOFLAGS wins and duplicates removed", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=mod", "PATH=/usr/bin", "GOFLAGS=-trimpath"}
		result := withReadonlyModEnv(envs)
		count := 0
		for _, e := range result {
			if strings.HasPrefix(e, "GOFLAGS=") {
				count++
				if e != "GOFLAGS=-trimpath -mod=readonly" {
					t.Fatalf("unexpected GOFLAGS %q", e)
				}
			}
		}
		if count != 1 {
			t.Fatalf("expected exactly one GOFLAGS, got %d: %v", count, result)
		}
	})

	t.Run("does not modify original slice", func(t *testing.T) {
		envs := []string{"GOFLAGS=-mod=mod"}
		_ = withReadonlyModEnv(envs)
		if envs[0] != "GOFLAGS=-mod=mod" {
			t.Fatalf("original slice was modified: %v", envs)
		}
	})
}
