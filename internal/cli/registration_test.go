package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestCLIRegistersOnboardingCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"--help"})
	assert.NoErr(t, err)
	for _, name := range []string{"init", "config", "models"} {
		assert.StrContains(t, stdout.String(), name)
	}
	assert.StrContains(t, stdout.String(), "suga config get model")
}

func TestConfigGroupHelpListsLeavesWithoutWriting(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("SUGA_HOME", home)
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config"})
	assert.NoErr(t, err)
	for _, name := range []string{"get", "set", "unset", "list", "path"} {
		assert.StrContains(t, stderr.String(), name)
	}
	_, statErr := os.Stat(home)
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestOnboardingLeavesReachTheirCappHandlers(t *testing.T) {
	t.Run("config get", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "get", "model"})
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "suga config get is not implemented")
	})

	t.Run("models json", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"models", "--json"})
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "suga models is not implemented")
	})
}

func TestConfigLeafHelpKeepsItsPositionalContract(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"config", "set", "--help"})
	assert.NoErr(t, err)
	assert.StrContains(t, stderr.String(), "key")
	assert.StrContains(t, stderr.String(), "value")
	assert.StrNotContains(t, stderr.String(), "SUBCOMMAND")
}
