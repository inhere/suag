package chat

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestInputHistoryLoadOrderAndRedaction(t *testing.T) {
	t.Setenv("SUGA_HOME", t.TempDir())
	h, err := newInputHistory("current", true, 3)
	assert.NoErr(t, err)
	assert.NoErr(t, h.add("old"))
	h.entries = append(h.entries, historyEntry{Workspace: "other", Text: "other"})
	assert.NoErr(t, h.add("token=secret"))
	got := h.lines()
	assert.Eq(t, []string{"old", "token=[REDACTED]", "other"}, got)
	loaded, err := newInputHistory("current", true, 3)
	assert.NoErr(t, err)
	assert.Eq(t, []string{"old", "token=[REDACTED]"}, loaded.lines())
}

func TestInputHistoryConcurrentAppend(t *testing.T) {
	t.Setenv("SUGA_HOME", t.TempDir())
	h, err := newInputHistory(filepath.Base(t.TempDir()), true, 1000)
	assert.NoErr(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); assert.NoErr(t, h.add(string(rune('a'+i)))) }(i)
	}
	wg.Wait()
	data, err := os.ReadFile(h.path)
	assert.NoErr(t, err)
	assert.True(t, len(data) > 0)
	assert.Eq(t, 20, len(h.entries))
}
