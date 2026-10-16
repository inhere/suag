package chat

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestInputHistoryLoadOrderAndRedaction(t *testing.T) {
	t.Setenv("SUGA_HOME", t.TempDir())
	t.Run("disabled history does not read or write", func(t *testing.T) {
		h, err := newInputHistory("current", false, 3)
		assert.NoErr(t, err)
		assert.False(t, h.enabled)
		assert.Eq(t, "", h.path)
		assert.NoErr(t, h.add("not persisted"))
	})
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

func TestInputHistoryTrimAndReload(t *testing.T) {
	t.Setenv("SUGA_HOME", t.TempDir())
	t.Run("keeps a margin before trimming", func(t *testing.T) {
		h, err := newInputHistory("work", true, 10)
		assert.NoErr(t, err)
		for i := 0; i < historyTrimLimit(10); i++ {
			assert.NoErr(t, h.add(string(rune('a'+i))))
		}
		data, err := os.ReadFile(h.path)
		assert.NoErr(t, err)
		assert.Eq(t, historyTrimLimit(10), len(strings.Split(strings.TrimSpace(string(data)), "\n")))
		assert.NoErr(t, h.add("last"))
		loaded, err := newInputHistory("work", true, 10)
		assert.NoErr(t, err)
		assert.Eq(t, 10, len(loaded.entries))
		assert.Eq(t, "last", loaded.entries[len(loaded.entries)-1].Text)
	})
	t.Run("does not lose another process append", func(t *testing.T) {
		h1, err := newInputHistory("work", true, 3)
		assert.NoErr(t, err)
		assert.NoErr(t, h1.add("one"))
		h2, err := newInputHistory("other", true, 3)
		assert.NoErr(t, err)
		assert.NoErr(t, h2.add("two"))
		assert.NoErr(t, h1.add("three"))
		assert.NoErr(t, h2.add("four"))
		loaded, err := newInputHistory("work", true, 3)
		assert.NoErr(t, err)
		assert.Eq(t, []string{"three", "two", "four"}, loaded.lines())
	})
}
