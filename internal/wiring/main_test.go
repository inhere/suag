package wiring

import (
	"os"
	"testing"
)

// TestMain keeps tests from running a hook command configured on the developer's
// machine (SUAG_HOOKS=off). SUAG_HOME also points at a throwaway directory so
// tests never touch ~/.suag.
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
