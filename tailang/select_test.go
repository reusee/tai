package tailang

import (
	"strings"
	"testing"
)

func TestSelect(t *testing.T) {
	env := NewEnv()
	src := `
		def c1 (make (chan_of int) 1)
		def c2 (make (chan_of int) 1)
		send c1 42

		def res 0
		select {
			case recv c1 v {
				set res v
			}
			case recv c2 v {
				set res -1
			}
		}
		res
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != 42 {
		t.Fatalf("expected 42, got %v", res)
	}
}

func TestSelectSend(t *testing.T) {
	env := NewEnv()
	src := `
		def c (make (chan_of int) 1)
		
		select {
			case send c 100 {
				"sent"
			}
			default {
				"default"
			}
		}
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "sent" {
		t.Fatalf("expected sent, got %v", res)
	}

	// Read back
	src2 := `recv c`
	tokenizer2 := NewTokenizer(strings.NewReader(src2))
	res2, err := env.Evaluate(tokenizer2)
	if err != nil {
		t.Fatal(err)
	}
	if res2 != 100 {
		t.Fatalf("expected 100, got %v", res2)
	}
}

func TestSelectDefault(t *testing.T) {
	env := NewEnv()
	src := `
		def c (make (chan_of int))
		select {
			case recv c v {
				"recv"
			}
			default {
				"default"
			}
		}
	`
	tokenizer := NewTokenizer(strings.NewReader(src))
	res, err := env.Evaluate(tokenizer)
	if err != nil {
		t.Fatal(err)
	}
	if res != "default" {
		t.Fatalf("expected default, got %v", res)
	}
}
