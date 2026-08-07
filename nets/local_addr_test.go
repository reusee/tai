package nets

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
)

func TestIsLocalAddr(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
		new(configs.NewLoader(nil, configs.LoaderConfig{})),
	).Call(func(
		isLocalAddr IsLocalAddr,
	) {
		yes, err := isLocalAddr("127.0.0.1:10000")
		if err != nil {
			t.Fatal(err)
		}
		if !yes {
			t.Fatal()
		}
		yes, err = isLocalAddr("qq.com")
		if err != nil {
			t.Fatal(err)
		}
		if yes {
			t.Fatal()
		}
	})
}

func TestIsLocalAddrCachesLookups(t *testing.T) {
	// Repeated calls for the same host must reuse the cached locality
	// determination instead of performing a new DNS lookup per call.
	// See TheoryOfLocalAddrCache.
	lookups := 0
	cache := newLocalAddrCache(func(host string) ([]net.IP, error) {
		lookups++
		switch host {
		case "private.example.com":
			return []net.IP{net.ParseIP("192.168.1.10")}, nil
		case "public.example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		default:
			return nil, fmt.Errorf("unexpected host %s", host)
		}
	})

	local, err := cache.isLocalAddr("private.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if !local {
		t.Fatal("private.example.com should be local")
	}

	// The second call for the same host must come from the cache.
	local, err = cache.isLocalAddr("private.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if !local {
		t.Fatal("cached result for private.example.com should be true")
	}
	if lookups != 1 {
		t.Fatalf("expected 1 lookup for repeated calls, got %d", lookups)
	}

	// A different host is resolved separately.
	local, err = cache.isLocalAddr("public.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	if local {
		t.Fatal("public.example.com should not be local")
	}
	if lookups != 2 {
		t.Fatalf("expected 2 lookups total, got %d", lookups)
	}
}

func TestIsLocalAddrDoesNotCacheFailures(t *testing.T) {
	// A failed lookup must not be cached: a transient DNS failure should
	// be retried on the next call rather than staying sticky.
	// See TheoryOfLocalAddrCache.
	lookups := 0
	cache := newLocalAddrCache(func(host string) ([]net.IP, error) {
		lookups++
		if lookups == 1 {
			return nil, errors.New("temporary DNS failure")
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	})

	local, err := cache.isLocalAddr("localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if local {
		t.Fatal("failed lookup must report not local")
	}

	// The second call retries the lookup and succeeds.
	local, err = cache.isLocalAddr("localhost:8080")
	if err != nil {
		t.Fatal(err)
	}
	if !local {
		t.Fatal("retried lookup should succeed and report local")
	}
	if lookups != 2 {
		t.Fatalf("expected 2 lookups (failure retried), got %d", lookups)
	}
}
