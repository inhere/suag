package main

import (
	"os"
	"testing"
)

// TestMain isolates tests from the real ~/.suga and from a configured hook command:
// SUGA_HOME points at a throwaway directory unless a test overrides it, and
// SUGA_HOOKS=off disables hooks.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "suga-test-home-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SUGA_HOME", home)
	_ = os.Setenv("SUGA_HOOKS", "off")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
