package axconfig

import (
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
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

func TestLoadUsesSuagConfigFromSuagHome(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	root := t.TempDir()
	t.Setenv("SUAG_HOME", root)
	t.Setenv("SUAG_CONFIG", "")
	t.Chdir(t.TempDir())
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte("llm:\n  model: home-model\n"), 0600))
	assert.NoErr(t, Load())
	assert.Eq(t, "home-model", ReadLLMProviderSettings().Model)
}

func TestLoadUsesExplicitConfigFile(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	root := t.TempDir()
	t.Setenv("SUAG_HOME", root)
	t.Chdir(t.TempDir())
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte("llm:\n  model: home-model\n"), 0600))
	configPath := filepath.Join(t.TempDir(), "chosen.yaml")
	assert.NoErr(t, os.WriteFile(configPath, []byte("llm:\n  model: chosen-model\n"), 0600))
	t.Setenv("SUAG_CONFIG", configPath)
	assert.NoErr(t, Load())
	assert.Eq(t, "chosen-model", ReadLLMProviderSettings().Model)
}

func TestLoadIgnoresWorkingDirectoryDotenv(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	root := t.TempDir()
	t.Setenv("SUAG_HOME", root)
	t.Setenv("SUAG_CONFIG", "")
	t.Setenv("SUAG_TEST_CWD_ONLY", "")
	t.Setenv("SUAG_TEST_CWD_ONLY2", "")
	cwd := t.TempDir()
	t.Chdir(cwd)
	other := t.TempDir()
	body := "SUAG_TEST_CWD_ONLY2=1\nSUAG_HOME=" + other + "\nSUAG_TEST_CWD_ONLY=1\n"
	assert.NoErr(t, os.WriteFile(filepath.Join(cwd, ".env"), []byte(body), 0600))
	assert.NoErr(t, os.WriteFile(filepath.Join(cwd, ".env.local"), []byte(body), 0600))
	assert.NoErr(t, Load())
	assert.Eq(t, "", os.Getenv("SUAG_TEST_CWD_ONLY2"))
	assert.Eq(t, "", os.Getenv("SUAG_TEST_CWD_ONLY"))
	assert.Eq(t, root, os.Getenv("SUAG_HOME"))
}

func TestLoadIgnoresWorkingDirectoryConfig(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	root := t.TempDir()
	t.Setenv("SUAG_HOME", root)
	t.Setenv("SUAG_CONFIG", "")
	cwd := t.TempDir()
	t.Chdir(cwd)
	assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte("llm:\n  model: home-model\n"), 0600))
	assert.NoErr(t, os.MkdirAll(filepath.Join(cwd, "config"), 0700))
	assert.NoErr(t, os.WriteFile(filepath.Join(cwd, "config", "config.yaml"), []byte("llm:\n  model: project-model\n"), 0600))
	assert.NoErr(t, os.WriteFile(filepath.Join(cwd, "config.yaml"), []byte("llm:\n  model: project-model\n"), 0600))
	assert.NoErr(t, Load())
	assert.Eq(t, "home-model", ReadLLMProviderSettings().Model)
}

func TestLoadSuagHomeDotenv(t *testing.T) {
	t.Run("ordinary variables apply", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		root := t.TempDir()
		t.Setenv("SUAG_HOME", root)
		t.Setenv("SUAG_TEST_HOME_VAR", "")
		t.Chdir(t.TempDir())
		assert.NoErr(t, os.WriteFile(filepath.Join(root, ".env"), []byte("SUAG_TEST_HOME_VAR=ok\n"), 0600))
		assert.NoErr(t, Load())
		assert.Eq(t, "ok", os.Getenv("SUAG_TEST_HOME_VAR"))
	})
	t.Run("SUAG_HOME and SUAG_CONFIG in .env do not move the root", func(t *testing.T) {
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		root := t.TempDir()
		other := t.TempDir()
		t.Setenv("SUAG_HOME", root)
		// Unset in the process env: the .env value must not stick afterwards.
		t.Setenv("SUAG_CONFIG", "")
		os.Unsetenv("SUAG_CONFIG")
		t.Chdir(t.TempDir())
		body := "SUAG_HOME=" + other + "\nSUAG_CONFIG=" + filepath.Join(other, "x.yaml") + "\n"
		assert.NoErr(t, os.WriteFile(filepath.Join(root, ".env"), []byte(body), 0600))
		assert.NoErr(t, Load())
		assert.Eq(t, root, os.Getenv("SUAG_HOME"))
		_, has := os.LookupEnv("SUAG_CONFIG")
		assert.False(t, has)
		got, err := ResolveSuagHome()
		assert.NoErr(t, err)
		assert.Eq(t, root, got)
	})
}
