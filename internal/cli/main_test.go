package cli

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
	// A developer's (or an eval's) model and config file must not decide
	// what the CLI tests resolve.
	_ = os.Unsetenv("SUAG_CONFIG")
	_ = os.Unsetenv("SUAG_MODEL")
	_ = os.Setenv("SUAG_HOOKS", "off")
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
