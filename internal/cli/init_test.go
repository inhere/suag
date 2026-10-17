package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestInitCommandUsesRealCappPath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("SUGA_HOME", home)
	var stdout, stderr bytes.Buffer
	assert.NoErr(t, newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"init"}))
	assert.StrContains(t, stdout.String(), "已写入")
	assert.StrContains(t, stderr.String(), filepath.Join(home, ".env"))
	_, err := os.Stat(filepath.Join(home, "config.yaml"))
	assert.NoErr(t, err)
}

func TestInitCommandForceReportsBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	configPath := filepath.Join(home, "config.yaml")
	assert.NoErr(t, os.WriteFile(configPath, []byte("model: openai/old\n"), 0o600))
	var stdout, stderr bytes.Buffer
	assert.NoErr(t, newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"init", "--force"}))
	assert.StrContains(t, stdout.String(), "已备份")
	assert.StrContains(t, stdout.String(), ".bak.")
}
