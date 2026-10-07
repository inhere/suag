package axconfig

import (
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"
)

func TestLoadReadsPrivateModelSettingsFromUserHome(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUAG_TEST_PRIVATE_API_KEY", "")
	t.Chdir(t.TempDir())

	privateDir := filepath.Join(home, ".suag")
	assert.NoErr(t, os.MkdirAll(privateDir, 0700))
	assert.NoErr(t, os.WriteFile(filepath.Join(privateDir, ".env"), []byte("SUAG_TEST_PRIVATE_API_KEY=test-key\n"), 0600))
	config := "llm:\n  model: private-model\n  api_key: \"${SUAG_TEST_PRIVATE_API_KEY}\"\n"
	assert.NoErr(t, os.WriteFile(filepath.Join(privateDir, "config.yaml"), []byte(config), 0600))

	assert.NoErr(t, Load())
	settings := ReadLLMProviderSettings()
	assert.Eq(t, "private-model", settings.Model)
	assert.Eq(t, "test-key", settings.APIKey)
}
