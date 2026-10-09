package cli

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

// setTestProvider points the preset openai provider at baseURL with a dummy key.
func setTestProvider(t *testing.T, baseURL string) {
	t.Helper()
	assert.NoErr(t, gkconfig.Set("providers", map[string]any{"openai": map[string]any{
		"type": "openai", "api_key": "local-test-key", "base_url": baseURL,
	}}))
}
