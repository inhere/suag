package axconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestReadIdleSettings(t *testing.T) {
	load := func(t *testing.T, body string) {
		t.Helper()
		gkconfig.Reset()
		t.Cleanup(gkconfig.Reset)
		root := t.TempDir()
		t.Setenv("SUAG_HOME", root)
		t.Setenv("SUAG_CONFIG", "")
		t.Chdir(t.TempDir())
		assert.NoErr(t, os.WriteFile(filepath.Join(root, "config.yaml"), []byte(body), 0600))
		assert.NoErr(t, Load())
	}
	t.Run("defaults", func(t *testing.T) {
		load(t, "model: openai/m\n")
		got, err := ReadIdleSettings()
		assert.NoErr(t, err)
		assert.Eq(t, 15*time.Minute, got.After)
		assert.Eq(t, 0.5, got.MinRatio)
	})
	for _, tc := range []struct {
		name, value string
		want        time.Duration
	}{
		{"seconds string", `"3s"`, 3 * time.Second},
		{"minutes", "15m", 15 * time.Minute},
		{"zero turns it off", "0", 0},
		{"zero string", `"0"`, 0},
		{"bare seconds", "90", 90 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			load(t, "context:\n  idle_after: "+tc.value+"\n  idle_min_ratio: 0.25\n")
			got, err := ReadIdleSettings()
			assert.NoErr(t, err)
			assert.Eq(t, tc.want, got.After)
			assert.Eq(t, 0.25, got.MinRatio)
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"bad duration", "context:\n  idle_after: soon\n"},
		{"negative duration", "context:\n  idle_after: -1s\n"},
		{"ratio above 1", "context:\n  idle_min_ratio: 1.5\n"},
		{"ratio not a number", "context:\n  idle_min_ratio: half\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			load(t, tc.body)
			_, err := ReadIdleSettings()
			assert.Err(t, err)
		})
	}
}
