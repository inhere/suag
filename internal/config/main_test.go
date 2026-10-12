package axconfig

import (
	"os"
	"testing"
)

// TestMain clears suag's own settings from the environment: a developer's
// SUAG_HOME / SUAG_CONFIG / SUAG_MODEL (or an eval's, inherited through the
// agent's shell) would otherwise decide what Load and ReadModelSettings see.
// Tests that need them set them with t.Setenv.
func TestMain(m *testing.M) {
	for _, key := range []string{"SUAG_HOME", "SUAG_CONFIG", "SUAG_MODEL"} {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
