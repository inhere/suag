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
