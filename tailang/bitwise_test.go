package tailang

import (
	"math/big"
	"strings"
	"testing"
)

func TestBitwise(t *testing.T) {
	env := NewEnv()
	run := func(src string, expected any) {
		t.Helper()
		tokenizer := NewTokenizer(strings.NewReader(src))
		res, err := env.Evaluate(tokenizer)
		if err != nil {
			t.Fatalf("src: %s, err: %v", src, err)
		}

		// Helper comparison for ints
		if iExp, ok := expected.(int); ok {
			if iRes, ok := res.(int); ok {
				if iRes != iExp {
					t.Fatalf("src: %s, expected %v, got %v", src, expected, res)
				}
				return
			}
		}
		// Helper for big ints
		if biExp, ok := expected.(*big.Int); ok {
			if biRes, ok := res.(*big.Int); ok {
				if biRes.Cmp(biExp) != 0 {
					t.Fatalf("src: %s, expected %v, got %v", src, expected, res)
				}
				return
			}
		}

		if res != expected {
			t.Fatalf("src: %s, expected %T %#v, got %T %#v", src, expected, expected, res, res)
		}
	}

	// Int
	run(`& 3 1`, 1)
	run(`| 1 2`, 3)
	run(`^ 3 1`, 2)
	run(`^ 1`, -2) // unary
	run(`&^ 3 1`, 2)
	run(`<< 1 2`, 4)
	run(`>> 4 2`, 1)
	run(`bit_not 0`, -1)

	// BigInt
	// 1 << 64
	large := new(big.Int).Lsh(big.NewInt(1), 64)
	// (1<<64) | 1
	expectedOr := new(big.Int).Or(large, big.NewInt(1))

	env.Define("large", large)
	run(`| large 1`, expectedOr)

	run(`& large 0`, big.NewInt(0))
}
