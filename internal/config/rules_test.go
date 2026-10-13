package axconfig

import (
	"os"
	"path/filepath"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestReadRuleFileName(t *testing.T) {
	load := func(t *testing.T, body string) {
		t.Helper()
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		root := t.TempDir()
		t.Setenv("SUGA_HOME", root)
		t.Setenv("SUGA_CONFIG", "")
		t.Chdir(t.TempDir())
		assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte(body), 0600))
		assert.NoErr(t, Load())
	}

	for _, tc := range []struct {
		name, body, want string
	}{
		{"unset defaults to AGENTS.md", "model: openai/m\n", "AGENTS.md"},
		{"empty value keeps the default", "model: openai/m\nrules:\n  file: \"\"\n", "AGENTS.md"},
		{"blank value keeps the default", "model: openai/m\nrules:\n  file: \"  \"\n", "AGENTS.md"},
		{"configured name", "model: openai/m\nrules:\n  file: CLAUDE.md\n", "CLAUDE.md"},
		{"surrounding space is trimmed", "model: openai/m\nrules:\n  file: \" CLAUDE.md \"\n", "CLAUDE.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			load(t, tc.body)
			got, err := ReadRuleFileName()
			assert.NoErr(t, err)
			assert.Eq(t, tc.want, got)
		})
	}

	for _, tc := range []struct{ name, body string }{
		{"path with separator", "model: openai/m\nrules:\n  file: docs/AGENTS.md\n"},
		{"windows separator", "model: openai/m\nrules:\n  file: '..\\AGENTS.md'\n"},
		{"parent directory", "model: openai/m\nrules:\n  file: ..\n"},
		{"not a string", "model: openai/m\nrules:\n  file: [a.md]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			load(t, tc.body)
			_, err := ReadRuleFileName()
			assert.Err(t, err)
			assert.StrContains(t, err.Error(), "rules.file")
		})
	}
}
