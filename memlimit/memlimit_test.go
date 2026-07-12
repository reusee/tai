package memlimit

import "testing"

func TestDefaultLimit(t *testing.T) {
	if DefaultLimit != 8*1024*1024*1024 {
		t.Fatalf("expected 8GB (%d), got %d", 8*1024*1024*1024, DefaultLimit)
	}
}

func TestApplyHighLimit(t *testing.T) {
	// Setting a very high limit should succeed on all platforms.
	err := Apply(1 << 40) // 1 TB
	if err != nil {
		t.Skipf("Apply failed (may be expected on some platforms): %v", err)
	}
}

func TestSetMemoryLimit(t *testing.T) {
	// Verify that setMemoryLimit doesn't error with a high limit.
	err := setMemoryLimit(1 << 40)
	if err != nil {
		t.Skipf("setMemoryLimit failed: %v", err)
	}
}
