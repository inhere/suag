package axconfig

import (
	"os"
	"testing"
)

// TestMain clears suga's own settings from the environment: a developer's
// SUGA_HOME / SUGA_CONFIG / SUGA_MODEL (or an eval's, inherited through the
// agent's shell) would otherwise decide what Load and ReadModelSettings see.
// Tests that need them set them with t.Setenv.
func TestMain(m *testing.M) {
	for _, key := range []string{"SUGA_HOME", "SUGA_CONFIG", "SUGA_MODEL", "SUGA_MAX_STEPS", "SUGA_MAX_OUTPUT_TOKENS"} {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
