package output

import (
	"os"
	"testing"
)

// TestMain keeps tests off the real ~/.suag and away from any configured hook command.
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
