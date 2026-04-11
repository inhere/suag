package session

import (
	"strings"
	"testing"
)

func TestNewID_UsesPrefixAndULID(t *testing.T) {
	t.Parallel()

	id := newID("turn_")
	if !strings.HasPrefix(id, "turn_") {
		t.Fatalf("newID() = %q, want prefix %q", id, "turn_")
	}

	raw := strings.TrimPrefix(id, "turn_")
	if len(raw) != 26 {
		t.Fatalf("newID() raw length = %d, want 26", len(raw))
	}

	for _, ch := range raw {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", ch) {
			t.Fatalf("newID() contains invalid ULID character %q in %q", ch, id)
		}
	}
}

func TestNewID_WithoutPrefixReturnsULID(t *testing.T) {
	t.Parallel()

	id := newID("")
	if len(id) != 26 {
		t.Fatalf("newID() length = %d, want 26", len(id))
	}
}
