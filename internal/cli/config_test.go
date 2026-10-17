package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suga/internal/config"
)

func TestConfigCommandsUseTheRealCappPath(t *testing.T) {
	t.Run("get reports the effective value and source", func(t *testing.T) {
		home := setupConfigCLI(t, "# keep this heading\nmodel: openai/old # keep inline\n")
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "get", "model"})
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "model = openai/old")
		assert.StrContains(t, stdout.String(), "source = config:")
		assert.StrContains(t, stdout.String(), filepath.Join(home, "config.yaml"))
	})

	t.Run("environment takes precedence without changing yaml", func(t *testing.T) {
		setupConfigCLI(t, "model: openai/config\n")
		t.Setenv("SUGA_MODEL", "deepseek/environment")
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "get", "model"})
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "model = deepseek/environment")
		assert.StrContains(t, stdout.String(), "source = env:SUGA_MODEL")
	})

	t.Run("set preserves comments and keeps one backup", func(t *testing.T) {
		home := setupConfigCLI(t, "# keep this heading\nmodel: openai/old # keep inline\n")
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "model", "openai/new"})
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "已写入")
		configPath := filepath.Join(home, "config.yaml")
		body, readErr := os.ReadFile(configPath)
		assert.Require(t, assert.NoErr(t, readErr))
		assert.StrContains(t, string(body), "# keep this heading")
		assert.StrContains(t, string(body), "model: openai/new # keep inline")
		assert.NoErr(t, os.WriteFile(configPath, body, 0o600))
		err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "model", "openai/latest"})
		assert.NoErr(t, err)
		_, statErr := os.Stat(configPath + ".bak.bak")
		assert.True(t, os.IsNotExist(statErr))
		backup, readErr := os.ReadFile(configPath + ".bak")
		assert.Require(t, assert.NoErr(t, readErr))
		assert.StrContains(t, string(backup), "openai/new")
	})

	t.Run("bad type and secret are input errors without writes", func(t *testing.T) {
		home := setupConfigCLI(t, "model: openai/old\n")
		configPath := filepath.Join(home, "config.yaml")
		before, err := os.ReadFile(configPath)
		assert.Require(t, assert.NoErr(t, err))

		var stdout, stderr bytes.Buffer
		err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "retry.max_attempts", "many"})
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, 2, ExitCode(err))

		err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "providers.openai.api_key", "do-not-write"})
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, 2, ExitCode(err))
		after, readErr := os.ReadFile(configPath)
		assert.Require(t, assert.NoErr(t, readErr))
		assert.Eq(t, string(before), string(after))
	})

	t.Run("unset removes a leaf but leaves environment precedence alone", func(t *testing.T) {
		home := setupConfigCLI(t, "model: openai/config\nchat:\n  history_size: 42\n")
		t.Setenv("SUGA_MODEL", "deepseek/environment")
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "unset", "model"})
		assert.NoErr(t, err)
		body, readErr := os.ReadFile(filepath.Join(home, "config.yaml"))
		assert.Require(t, assert.NoErr(t, readErr))
		assert.NotContains(t, string(body), "model:")

		stdout.Reset()
		err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "get", "model"})
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "model = deepseek/environment")
	})

	t.Run("explicit config is the write target", func(t *testing.T) {
		home := t.TempDir()
		external := filepath.Join(t.TempDir(), "chosen.yaml")
		t.Setenv("SUGA_HOME", home)
		t.Setenv("SUGA_CONFIG", external)
		assert.NoErr(t, os.WriteFile(external, []byte("model: openai/external\n"), 0o600))
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		assert.NoErr(t, appconfig.Load())
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "model", "deepseek/changed"})
		assert.NoErr(t, err)
		body, readErr := os.ReadFile(external)
		assert.Require(t, assert.NoErr(t, readErr))
		assert.StrContains(t, string(body), "deepseek/changed")
		_, statErr := os.Stat(filepath.Join(home, "config.yaml"))
		assert.True(t, os.IsNotExist(statErr))
	})
	t.Run("path and list are stable command outputs", func(t *testing.T) {
		home := setupConfigCLI(t, "model: openai/config\n")
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "path"})
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "home = "+home)
		assert.StrContains(t, stdout.String(), "source = home")

		stdout.Reset()
		err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "list"})
		assert.NoErr(t, err)
		assert.StrContains(t, stdout.String(), "model = openai/config")
		assert.StrNotContains(t, stdout.String(), "do-not-write")
	})
}

func setupConfigCLI(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	t.Setenv("SUGA_CONFIG", "")
	assert.NoErr(t, os.WriteFile(filepath.Join(home, "config.yaml"), []byte(content), 0o600))
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	assert.NoErr(t, appconfig.Load())
	return home
}
