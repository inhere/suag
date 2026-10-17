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

func TestConfigPreferredToolsUseSharedRegistry(t *testing.T) {
	home := setupConfigCLI(t, "# keep this heading\nmodel: openai/test\n")
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{
		"config", "set", "tools.preferred", "[{name: rg, kind: command, when: 'search text'}]",
	})
	assert.NoErr(t, err)
	body, readErr := os.ReadFile(filepath.Join(home, "config.yaml"))
	assert.Require(t, assert.NoError(t, readErr))
	assert.StrContains(t, string(body), "# keep this heading")
	assert.StrContains(t, string(body), "preferred")

	stdout.Reset()
	err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "get", "tools.preferred"})
	assert.NoErr(t, err)
	assert.StrContains(t, stdout.String(), "rg")
	assert.StrContains(t, stdout.String(), "search text")

	before, readErr := os.ReadFile(filepath.Join(home, "config.yaml"))
	assert.Require(t, assert.NoError(t, readErr))
	for _, value := range []string{
		"[",
	} {
		stdout.Reset()
		err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "tools.preferred", value})
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, 2, ExitCode(err))
		after, readErr := os.ReadFile(filepath.Join(home, "config.yaml"))
		assert.Require(t, assert.NoError(t, readErr))
		assert.Eq(t, string(before), string(after))
	}

	err = newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "unset", "tools.preferred"})
	assert.NoError(t, err)
	body, readErr = os.ReadFile(filepath.Join(home, "config.yaml"))
	assert.Require(t, assert.NoError(t, readErr))
	assert.NotContains(t, string(body), "preferred")
}
