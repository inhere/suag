package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestCLIRegistersPersonalCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"--help"})
	assert.NoErr(t, err)
	for _, name := range []string{"run", "chat", "acp"} {
		assert.Eq(t, true, strings.Contains(stdout.String(), name))
	}
}

func TestACPRejectsPositionalsWithoutWritingProtocolOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"acp", "extra"})
	assert.Err(t, err)
	assert.Eq(t, true, strings.Contains(err.Error(), "does not accept arguments"))
	assert.Eq(t, "", stdout.String())
}

func TestVersionFlagsWriteConfiguredOutputAndDoNotAffectNextApp(t *testing.T) {
	for _, flag := range []string{"--version", "-V"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{flag})
			assert.NoErr(t, err)
			assert.Eq(t, Version+"\n", stdout.String())
			assert.Eq(t, "", stderr.String())
		})
	}

	var stdout, stderr bytes.Buffer
	err := newCLI(context.Background(), strings.NewReader(""), &stdout, &stderr).RunWithArgs([]string{"run", "--help"})
	assert.NoErr(t, err)
	assert.Eq(t, true, strings.Contains(stderr.String(), "--prompt"))
	assert.Eq(t, "", stdout.String())
}

func TestFormatVersionIncludesAvailableBuildMetadata(t *testing.T) {
	assert.Eq(t, "dev", formatVersion("dev", "", ""))
	assert.Eq(t, "1.2.3 (commit abc123, built 2026-10-07)", formatVersion("1.2.3", "abc123", "2026-10-07"))
}
