package memory

import (
	"errors"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestRouter(t *testing.T) {
	user, project := newFakeStore("files"), newFakeStore("gofer")
	r := NewRouter()
	assert.NoErr(t, r.Register(ScopeUser, user))
	assert.NoErr(t, r.Register(ScopeProject, project))

	t.Run("dispatches by scope kind", func(t *testing.T) {
		_, err := r.Write(ctx, UserScope(), addOp("u", "b"))
		assert.NoErr(t, err)
		_, err = r.Write(ctx, ProjectScope("a"), addOp("p", "b"))
		assert.NoErr(t, err)
		_, _ = r.Index(ctx, ProjectScope("b"))
		_, _ = r.Get(ctx, UserScope(), "u")
		_, _ = r.Search(ctx, ProjectScope("a"), "p", 3)
		assert.Eq(t, map[string]int{"write": 1, "get": 1}, user.calls)
		assert.Eq(t, map[string]int{"write": 1, "index": 1, "search": 1}, project.calls)
	})
	t.Run("unregistered scope fails clearly", func(t *testing.T) {
		people := Scope{Kind: ScopePeople, Key: "bob"}
		_, err := r.Index(ctx, people)
		assert.True(t, errors.Is(err, ErrNoBackend))
		assert.ErrSubMsg(t, err, "people:bob")
		_, err = r.Get(ctx, people, "x")
		assert.True(t, errors.Is(err, ErrNoBackend))
		_, err = r.Write(ctx, people, addOp("a", "b"))
		assert.True(t, errors.Is(err, ErrNoBackend))
		_, err = r.Search(ctx, people, "a", 1)
		assert.True(t, errors.Is(err, ErrNoBackend))
	})
	t.Run("backends lists names", func(t *testing.T) {
		assert.Eq(t, []RouteInfo{{Scope: "project", Backend: "gofer"}, {Scope: "user", Backend: "files"}}, r.Backends())
		g := NewGoverned(newFakeStore("exec"), Policy{})
		assert.NoErr(t, r.Register(ScopePeople, g))
		assert.Eq(t, RouteInfo{Scope: "people", Backend: "exec"}, r.Backends()[0], "Governed reports its backend name")
	})
	t.Run("register validates", func(t *testing.T) {
		assert.True(t, errors.Is(r.Register("team", user), ErrInvalid))
		assert.True(t, errors.Is(r.Register(ScopeUser, nil), ErrInvalid))
	})
	t.Run("unnamed backend", func(t *testing.T) {
		assert.Eq(t, "unknown", BackendName(unnamed{}))
	})
}

type unnamed struct{ Store }
