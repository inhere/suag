package axconfig

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestReadFoldSettings(t *testing.T) {
	t.Run("defaults: off, keep 5", func(t *testing.T) {
		loadYAML(t, "")
		s, err := ReadFoldSettings()
		assert.NoErr(t, err)
		assert.Eq(t, FoldSettings{Every: 0, Keep: 5}, s)
	})
	t.Run("configured on", func(t *testing.T) {
		loadYAML(t, "context:\n  fold_every: 10\n  fold_keep: 2\n")
		s, err := ReadFoldSettings()
		assert.NoErr(t, err)
		assert.Eq(t, FoldSettings{Every: 10, Keep: 2}, s)
	})
	t.Run("invalid values", func(t *testing.T) {
		for doc, want := range map[string]string{
			"context:\n  fold_every: -1\n": "context.fold_every must be an integer >= 0",
			"context:\n  fold_keep: 0\n":   "context.fold_keep must be an integer >= 1",
			"context:\n  fold_keep: x\n":   "context.fold_keep must be an integer >= 1",
		} {
			loadYAML(t, doc)
			_, err := ReadFoldSettings()
			assert.ErrMsgContains(t, err, want)
		}
	})
}
