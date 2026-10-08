package app

import (
	"os"
	"testing"
)

// TestMain keeps chat tests from calling a real `gofer hook` on PATH, which would
// register sessions and work items with whatever Gofer server the developer uses.
// SUAG_HOME also points at a throwaway directory so tests never touch ~/.suag.
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
