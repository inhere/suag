package axconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suga/pkg/agentcore/llm"
)

func TestReadRetryPolicy(t *testing.T) {
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
	t.Run("defaults", func(t *testing.T) {
		load(t, "model: openai/m\n")
		got, err := ReadRetryPolicy()
		assert.NoErr(t, err)
		assert.Eq(t, llm.DefaultRetryPolicy, got)
	})
	t.Run("set fields, the rest default", func(t *testing.T) {
		load(t, "model: openai/m\nretry:\n  max_attempts: 3\n  base_delay: 500ms\n  max_delay: 20\n")
		got, err := ReadRetryPolicy()
		assert.NoErr(t, err)
		assert.Eq(t, 3, got.MaxAttempts)
		assert.Eq(t, 500*time.Millisecond, got.BaseDelay)
		assert.Eq(t, 20*time.Second, got.MaxDelay)
		assert.Eq(t, llm.DefaultRetryPolicy.ResetAfter, got.ResetAfter)
	})
	t.Run("every provider gets it", func(t *testing.T) {
		load(t, "model: openai/m\nretry:\n  max_attempts: 1\nproviders:\n  gw:\n    type: anthropic\n    base_url: http://x\n")
		ps, err := ReadProviders()
		assert.NoErr(t, err)
		assert.Eq(t, 1, ps["gw"].Retry.MaxAttempts)
		assert.Eq(t, 1, ps["openai"].Retry.MaxAttempts)
	})
	for _, tc := range []struct{ name, body, want string }{
		{"zero attempts", "retry:\n  max_attempts: 0\n", "retry.max_attempts must be a whole number >= 1"},
		{"bad duration", "retry:\n  base_delay: soon\n", "retry.base_delay"},
		{"zero delay", "retry:\n  max_delay: 0\n", "retry.max_delay: must be above 0"},
		{"max below base", "retry:\n  base_delay: 10s\n  max_delay: 5s\n", "below retry.base_delay"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			load(t, "model: openai/m\n"+tc.body)
			_, err := ReadRetryPolicy()
			assert.ErrMsgContains(t, err, tc.want)
			_, err = ReadProviders()
			assert.ErrMsgContains(t, err, tc.want)
		})
	}
}
