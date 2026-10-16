package api

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestSessionID(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		assert.Eq(t, "", SessionIDFrom(context.Background()))
	})
	t.Run("set", func(t *testing.T) {
		ctx := WithSessionID(context.Background(), "sess-1")
		assert.Eq(t, "sess-1", SessionIDFrom(ctx))
	})
	t.Run("the latest call wins", func(t *testing.T) {
		ctx := WithSessionID(WithSessionID(context.Background(), "old"), "new")
		assert.Eq(t, "new", SessionIDFrom(ctx))
	})
	t.Run("empty id is stored as empty", func(t *testing.T) {
		assert.Eq(t, "", SessionIDFrom(WithSessionID(context.Background(), "")))
	})
}
