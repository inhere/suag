package files

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/memory"
)

func TestEntriesEnumeratesSearchableFiles(t *testing.T) {
	dir := t.TempDir()
	s := newStore(t, map[memory.Scope]Layout{user: DirLayout{Dir: dir}})
	first, err := s.Write(context.Background(), user, add("First", "body", memory.KindUser))
	assert.Require(t, assert.NoErr(t, err))
	second, err := s.Write(context.Background(), user, add("Second", "body", memory.KindProject))
	assert.Require(t, assert.NoErr(t, err))

	entries, err := s.Entries(context.Background(), user)
	assert.Require(t, assert.NoErr(t, err))
	assert.Len(t, entries, 2)
	ids := map[string]bool{}
	for _, entry := range entries {
		ids[entry.ID] = true
	}
	assert.True(t, ids[first.ID])
	assert.True(t, ids[second.ID])
}
