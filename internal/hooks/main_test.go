package hooks

import (
	"os"
	"testing"
)

// TestMain keeps tests from running a hook command configured on the developer's
// machine (SUGA_HOOKS=off). SUGA_HOME also points at a throwaway directory so
// tests never touch ~/.suga.
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
