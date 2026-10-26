package main

import "testing"

func TestTrimOneLineEnding(t *testing.T) {
	tests := map[string]string{
		"secret\n":   "secret",
		"secret\r\n": "secret",
		"secret":     "secret",
		"secret\n\n": "secret\n",
	}
	for input, expected := range tests {
		if actual := string(trimOneLineEnding([]byte(input))); actual != expected {
			t.Fatalf("trimOneLineEnding(%q)=%q, want %q", input, actual, expected)
		}
	}
}
