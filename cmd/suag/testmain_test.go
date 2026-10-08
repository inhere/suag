package main

import (
	"os"
	"testing"
)

// TestMain isolates tests from the real ~/.suag and from a host `gofer hook`:
// SUAG_HOME points at a throwaway directory unless a test overrides it.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "suag-test-home-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SUAG_HOME", home)
	_ = os.Setenv("SUAG_GOFER_HOOKS", "off")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
