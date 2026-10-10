package agentcore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suag/pkg/agentcore"
	agentcoretesting "github.com/inhere/suag/pkg/agentcore/testing"
)

func TestMemoryStorageSuite(t *testing.T) {
	agentcoretesting.RunStorageSuite(t, func(*testing.T) agentcore.Storage {
		return agentcore.NewMemoryStorage()
	})
}

func TestMemoryStorage(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown run is a storage miss", func(t *testing.T) {
		s := agentcore.NewMemoryStorage()
		_, err := s.LoadRun(ctx, "nope")
		assertMiss(t, err)
		_, err = s.LoadMessages(ctx, "nope")
		assertMiss(t, err)
		_, err = s.LoadArtifacts(ctx, "nope")
		assertMiss(t, err)
	})

	t.Run("run known only by messages has no snapshot", func(t *testing.T) {
		s := agentcore.NewMemoryStorage()
		assert.NoErr(t, s.AppendMessages(ctx, "r1"))
		_, err := s.LoadRun(ctx, "r1")
		assertMiss(t, err)
		msgs, err := s.LoadMessages(ctx, "r1")
		assert.NoErr(t, err)
		assert.Eq(t, 0, len(msgs))
		artifacts, err := s.LoadArtifacts(ctx, "r1")
		assert.NoErr(t, err)
		assert.NotNil(t, artifacts)
		assert.Eq(t, 0, len(artifacts))
	})

	t.Run("invalid run id is a storage error", func(t *testing.T) {
		s := agentcore.NewMemoryStorage()
		for _, id := range []string{"", " ", ".", "..", "../x", "a/b", `a\b`, "/abs"} {
			var storageErr *agentcore.StorageError
			assert.True(t, errors.As(s.SaveRun(ctx, &agentcore.RunState{RunID: id}), &storageErr), id)
			_, err := s.LoadMessages(ctx, id)
			assert.True(t, errors.As(err, &storageErr), id)
		}
		var storageErr *agentcore.StorageError
		assert.True(t, errors.As(s.SaveRun(ctx, nil), &storageErr))
	})

	t.Run("saved values are isolated from callers", func(t *testing.T) {
		s := agentcore.NewMemoryStorage()
		state := &agentcore.RunState{RunID: "r1", Metadata: map[string]any{"k": "v"}}
		msg := agentcore.Message{Role: "user", Content: "hi", Metadata: map[string]any{"k": "v"}}
		artifacts := []agentcore.Artifact{{Kind: "k", Metadata: map[string]any{"k": "v"}}}
		assert.NoErr(t, s.SaveRun(ctx, state))
		assert.NoErr(t, s.AppendMessages(ctx, "r1", msg))
		assert.NoErr(t, s.SaveArtifacts(ctx, "r1", artifacts))
		state.Metadata["k"], msg.Metadata["k"], artifacts[0].Metadata["k"] = "changed", "changed", "changed"

		loaded, err := s.LoadRun(ctx, "r1")
		assert.NoErr(t, err)
		assert.Eq(t, "v", loaded.Metadata["k"])
		loaded.Metadata["k"] = "changed"
		again, err := s.LoadRun(ctx, "r1")
		assert.NoErr(t, err)
		assert.Eq(t, "v", again.Metadata["k"])

		msgs, err := s.LoadMessages(ctx, "r1")
		assert.NoErr(t, err)
		assert.Eq(t, "v", msgs[0].Metadata["k"])
		gotArtifacts, err := s.LoadArtifacts(ctx, "r1")
		assert.NoErr(t, err)
		assert.Eq(t, "v", gotArtifacts[0].Metadata["k"])
	})

	t.Run("concurrent use is safe", func(t *testing.T) {
		s := agentcore.NewMemoryStorage()
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id := fmt.Sprintf("r%d", i%2)
				_ = s.SaveRun(ctx, &agentcore.RunState{RunID: id})
				_ = s.AppendMessages(ctx, id, agentcore.Message{Role: "user", Content: "x"})
				_, _ = s.LoadMessages(ctx, id)
				_, _ = s.LoadRun(ctx, id)
			}()
		}
		wg.Wait()
		msgs, err := s.LoadMessages(ctx, "r0")
		assert.NoErr(t, err)
		assert.Eq(t, 4, len(msgs))
	})
}

func assertMiss(t *testing.T, err error) {
	t.Helper()
	var storageErr *agentcore.StorageError
	assert.True(t, errors.As(err, &storageErr))
	assert.True(t, errors.Is(err, os.ErrNotExist))
}
