package output

import (
	"os"
	"testing"
)

// TestMain keeps tests off the real ~/.suga and away from any configured hook command.
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
