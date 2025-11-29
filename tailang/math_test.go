package tailang

import (
	"math/big"
	"strings"
	"testing"
)

func TestBigInt(t *testing.T) {
	env := NewEnv()
	run := func(src string) any {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		return res
	}

	// Parsing large int
	res := run(`123456789012345678901234567890`)
	if _, ok := res.(*big.Int); !ok {
		t.Fatalf("expected big.Int, got %T", res)
	}

	// Overflow promotion
	// MaxInt64 is 9223372036854775807
	res = run(`+ 9223372036854775807 1`)
	if _, ok := res.(*big.Int); !ok {
		t.Fatalf("expected big.Int after overflow, got %T: %v", res, res)
	}

	// Big Int arithmetic
	res = run(`
		def a 12345678901234567890
		def b 1
		+ a b
	`)
	if bi, ok := res.(*big.Int); !ok || bi.String() != "12345678901234567891" {
		t.Fatalf("big int arithmetic failed: %v", res)
	}
}

func TestBigFloat(t *testing.T) {
	env := NewEnv()
	run := func(src string) any {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		return res
	}

	// Mixed arithmetic
	res := run(`+ 1.5 2`)
	if _, ok := res.(float64); !ok {
		t.Fatalf("expected float64 for small float op, got %T", res)
	}
	if res.(float64) != 3.5 {
		t.Fatalf("expected 3.5, got %v", res)
	}

	res = run(`+ 1.5 123456789012345678901234567890`)
	if _, ok := res.(*big.Float); !ok {
		t.Fatalf("expected big.Float for mixed op, got %T", res)
	}
}

func TestBigComparisons(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected bool) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}
		if res != expected {
			t.Fatalf("src: %s, expected %v, got %v", src, expected, res)
		}
	}

	run(`< 12345678901234567890 12345678901234567891`, true)
	run(`> 12345678901234567891 12345678901234567890`, true)
	run(`== 12345678901234567890 12345678901234567890`, true)
}

func TestMathStdLib(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected float64) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}

		var fRes float64
		switch v := res.(type) {
		case float64:
			fRes = v
		case int:
			fRes = float64(v)
		default:
			t.Fatalf("expected number, got %T", res)
		}

		diff := fRes - expected
		if diff < 0 {
			diff = -diff
		}
		if diff > 1e-9 {
			t.Fatalf("src: %s, expected %v, got %v", src, expected, res)
		}
	}

	run(`math.pow 2 3`, 8.0)
	run(`math.sqrt 16`, 4.0)
	run(`math.abs -10.5`, 10.5)
	run(`math.floor 1.9`, 1.0)
	run(`math.ceil 1.1`, 2.0)
}

func TestMathUnderflow(t *testing.T) {
	env := NewEnv()
	// MinInt64 = -9223372036854775808
	// - MinInt64 1 -> underflow to BigInt
	src := `- -9223372036854775808 1`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.(*big.Int); !ok {
		t.Fatalf("expected big.Int after underflow, got %T", res)
	}
}
