package personal

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/internal/personal/store"
)

// An explicitly supplied empty append prompt clears the saved one; an unset
// one keeps it.
func TestRunnerAppendPromptClearedByExplicitEmpty(t *testing.T) {
	ctx := context.Background()
	const clearedNotice = "附加提示已清除"
	cases := []struct {
		name       string
		set        bool
		wantStored string
		wantNotice int
	}{
		{"explicit empty clears", true, "", 1},
		{"unset keeps", false, "orig", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, err := store.Open(newStorePath(t))
			assert.NoErr(t, err)
			t.Cleanup(func() { _ = repo.Close() })
			provider := &rulesProvider{}
			creator, err := New(Config{Repository: repo, Provider: provider, AppendSystemPrompt: "orig"})
			assert.NoErr(t, err)
			model, err := creator.NewSession(ctx, "clear-session", "test")
			assert.NoErr(t, err)
			resumed, err := New(Config{Repository: repo, Provider: provider, AppendSystemPromptSet: tc.set})
			assert.NoErr(t, err)
			_, err = resumed.Run(ctx, RunRequest{SessionID: model.ID, Prompt: "one"})
			assert.NoErr(t, err)
			stored, err := repo.GetSession(ctx, model.ID)
			assert.NoErr(t, err)
			assert.Eq(t, tc.wantStored, stored.Meta["personal_append_prompt"])
			assert.Eq(t, tc.wantNotice, countNotices(provider.requests[0].Messages, clearedNotice))
			if tc.set {
				assert.Eq(t, false, strings.Contains(provider.requests[0].System, "orig"))
			}
		})
	}
}
