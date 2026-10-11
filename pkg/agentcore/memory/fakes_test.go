package memory

import (
	"context"
	"sort"
	"strings"
	"time"
)

// fakeStore is an in-memory backend that counts calls so tests can assert
// which pipeline steps ran.
type fakeStore struct {
	name    string
	entries map[string]Entry
	calls   map[string]int
	ops     []Op
	// extraLines / extraBytes inflate the index for budget tests.
	extraLines int
	extraBytes int
	// raw overrides returned by reads (to test sanitizing untrusted output).
	rawIndex []IndexLine
	rawHits  []Hit
}

func newFakeStore(name string) *fakeStore {
	return &fakeStore{name: name, entries: map[string]Entry{}, calls: map[string]int{}}
}

func (f *fakeStore) BackendName() string { return f.name }

func (f *fakeStore) Index(_ context.Context, s Scope) (Index, error) {
	f.calls["index"]++
	ix := Index{Scope: s}
	if f.rawIndex != nil {
		ix.Lines = f.rawIndex
		return ix, nil
	}
	ids := make([]string, 0, len(f.entries))
	for id := range f.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := f.entries[id]
		ix.Lines = append(ix.Lines, IndexLine{ID: id, Title: e.Title, Kind: e.Kind})
	}
	for i := 0; i < f.extraLines; i++ {
		ix.Lines = append(ix.Lines, IndexLine{ID: "pad", Title: "pad", Kind: KindUser})
	}
	ix.Bytes = len(ix.Render()) + f.extraBytes
	return ix, nil
}

func (f *fakeStore) Get(_ context.Context, _ Scope, id string) (Entry, error) {
	f.calls["get"]++
	e, ok := f.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return e, nil
}

func (f *fakeStore) Write(_ context.Context, _ Scope, op Op) (Entry, error) {
	f.calls["write"]++
	f.ops = append(f.ops, op)
	switch op.Action {
	case ActionAdd:
		id := UniqueID(Slug(op.Title), func(id string) bool { _, ok := f.entries[id]; return ok })
		e := Entry{ID: id, Title: op.Title, Body: op.Body, Kind: op.Kind, Updated: time.Now()}
		f.entries[id] = e
		return e, nil
	case ActionReplace:
		old, ok := f.entries[op.ID]
		if !ok {
			return Entry{}, ErrNotFound
		}
		e := Entry{ID: op.ID, Title: op.Title, Body: op.Body, Kind: op.Kind, Updated: time.Now()}
		if e.Kind == "" {
			e.Kind = old.Kind
		}
		f.entries[op.ID] = e
		return e, nil
	default:
		e, ok := f.entries[op.ID]
		if !ok {
			return Entry{}, ErrNotFound
		}
		delete(f.entries, op.ID)
		return e, nil
	}
}

func (f *fakeStore) Search(_ context.Context, _ Scope, q string, _ int) ([]Hit, error) {
	f.calls["search"]++
	if f.rawHits != nil {
		return f.rawHits, nil
	}
	var hits []Hit
	for _, e := range f.entries {
		if strings.Contains(strings.ToLower(e.Title+" "+e.Body), strings.ToLower(q)) {
			hits = append(hits, Hit{ID: e.ID, Title: e.Title, Kind: e.Kind, Snippet: e.Body})
		}
	}
	return hits, nil
}

// fakeProv is an in-memory ProvenanceStore.
type fakeProv struct {
	calls      map[string]int
	tombstones map[string]bool // fingerprint -> removed
	records    []Entry
	backends   []string
	fps        []string
}

func newFakeProv() *fakeProv {
	return &fakeProv{calls: map[string]int{}, tombstones: map[string]bool{}}
}

func (p *fakeProv) Record(_ context.Context, backend string, _ Scope, e Entry, fp string) error {
	p.calls["record"]++
	p.records = append(p.records, e)
	p.backends = append(p.backends, backend)
	p.fps = append(p.fps, fp)
	return nil
}

func (p *fakeProv) Tombstone(_ context.Context, backend string, _ Scope, _ string, fp string) error {
	p.calls["tombstone"]++
	p.backends = append(p.backends, backend)
	p.tombstones[fp] = true
	return nil
}

func (p *fakeProv) IsTombstoned(_ context.Context, _ string, _ Scope, fp string) (bool, error) {
	p.calls["is_tombstoned"]++
	return p.tombstones[fp], nil
}

// provStore is a backend that keeps its own provenance.
type provStore struct {
	*fakeStore
	*fakeProv
}
