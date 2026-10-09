package personal

import (
	"os"
	"testing"
)

// TestMain isolates tests from the real ~/.suag and from a configured hook command:
// SUAG_HOME points at a throwaway directory unless a test overrides it, and
// SUAG_HOOKS=off disables hooks.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "suag-test-home-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("SUAG_HOME", home)
	_ = os.Setenv("SUAG_HOOKS", "off")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
