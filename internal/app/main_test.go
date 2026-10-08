package app

import (
	"os"
	"testing"
)

// TestMain keeps chat tests from calling a real `gofer hook` on PATH, which would
// register sessions and work items with whatever Gofer server the developer uses.
func TestMain(m *testing.M) {
	_ = os.Setenv("SUAG_GOFER_HOOKS", "off")
	os.Exit(m.Run())
}
