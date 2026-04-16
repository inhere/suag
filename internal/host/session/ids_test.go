package session

import (
	"strings"
	"testing"

	"github.com/gookit/goutil/testutil/assert"
)

func TestNewID_UsesPrefix(t *testing.T) {
	t.Parallel()

	id := newID("turn_")
	if !strings.HasPrefix(id, "turn_") {
		t.Fatalf("newID() = %q, want prefix %q", id, "turn_")
	}

	raw := strings.TrimPrefix(id, "turn_")
	assert.LenGt(t, raw, 10)
	assert.Len(t, raw, 15)
}

func TestNewID_WithoutPrefixReturnsCompactID(t *testing.T) {
	t.Parallel()

	id := newID("")
	assert.LenGt(t, id, 8)
	assert.Len(t, id, 15)
}

func TestNewID_AvoidsCollisionWithinBurst(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{}, 2048)
	for range 2048 {
		id := newID("run_")
		if _, ok := seen[id]; ok {
			t.Fatalf("newID() duplicated id %q", id)
		}
		seen[id] = struct{}{}
	}
}
