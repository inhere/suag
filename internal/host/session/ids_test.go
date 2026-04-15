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
}

func TestNewID_WithoutPrefixReturnsULID(t *testing.T) {
	t.Parallel()

	id := newID("")
	assert.LenGt(t, id, 8)
}
