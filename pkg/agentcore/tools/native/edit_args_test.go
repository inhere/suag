package native

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// TestEditListEmptyFormCountsAsAbsent: models with strict tool schemas fill every
// optional field, so an empty edits next to an old_string/new_string pair (or an
// empty old_string next to edits) is the other form left blank, not both forms.
func TestEditListEmptyFormCountsAsAbsent(t *testing.T) {
	pair := func(edits any) map[string]any {
		return map[string]any{"path": "a.txt", "old_string": "a", "new_string": "b", "edits": edits}
	}
	for name, edits := range map[string]any{"empty array": []any{}, "empty string": "", "empty json": "[]", "null": nil} {
		t.Run(name, func(t *testing.T) {
			list, err := EditList(pair(edits))
			assert.NoErr(t, err)
			assert.Len(t, list, 1)
			assert.Eq(t, "a", list[0]["old_string"])
		})
	}
	t.Run("edits with an empty old_string", func(t *testing.T) {
		list, err := EditList(map[string]any{"old_string": "", "new_string": "",
			"edits": []any{map[string]any{"old_string": "x", "new_string": "y"}}})
		assert.NoErr(t, err)
		assert.Len(t, list, 1)
		assert.Eq(t, "x", list[0]["old_string"])
	})
	t.Run("both forms filled is still an error", func(t *testing.T) {
		_, err := EditList(pair([]any{map[string]any{"old_string": "x", "new_string": "y"}}))
		assert.ErrMsgContains(t, err, "not both")
	})
}
