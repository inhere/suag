package personal

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
	"github.com/inhere/suga/pkg/agentcore/tools/native"
)

func TestLedgerWorkspaceScopePushesCwdToStore(t *testing.T) {
	f := newLedgerFixture(t)
	root := filepath.Join(string(filepath.Separator), "repo")
	f.session(t, "current", "")
	f.session(t, "exact", root)
	f.session(t, "sub", filepath.Join(root, "sub"))
	f.session(t, "sibling", root+"2")
	f.session(t, "case", filepath.Join(string(filepath.Separator)+"REPO", "case"))
	for _, id := range []string{"current", "exact", "sub", "sibling", "case"} {
		f.message(t, id, "user", "workspace-only token in "+id, "")
	}

	ctx := withObserveScope(context.Background(), "current", "turn_1")
	hits, err := f.ledger.Search(ctx, native.LedgerQuery{Query: "workspace-only token", Scope: native.LedgerScopeWorkspace})
	assert.NoErr(t, err)
	assert.Eq(t, []string{"current/1"}, hitRefs(hits.Hits))

	// A current session with no cwd remains visible, while the store applies
	// the workspace boundary to every other session in the same SQL query.
	f.session(t, "workspace-current", root)
	f.message(t, "workspace-current", "user", "workspace-only token in workspace-current", "")
	f.ledger.foldCase = false
	ctx = withObserveScope(context.Background(), "workspace-current", "turn_2")
	hits, err = f.ledger.Search(ctx, native.LedgerQuery{Query: "workspace-only token", Scope: native.LedgerScopeWorkspace})
	assert.NoErr(t, err)
	assert.Eq(t, []string{"exact/1", "sub/1", "workspace-current/1"}, hitRefs(hits.Hits))

	// FoldCase is injected on the ledger to make this assertion independent of
	// the host GOOS; production enables it only for Windows.
	f.ledger.foldCase = true
	hits, err = f.ledger.Search(ctx, native.LedgerQuery{Query: "workspace-only token", Scope: native.LedgerScopeWorkspace})
	assert.NoErr(t, err)
	assert.Eq(t, []string{"case/1", "exact/1", "sub/1", "workspace-current/1"}, hitRefs(hits.Hits))

	// A non-existent current session remains a restricted SQL predicate rather
	// than the empty SessionIDs value that historically meant "all".
	storeHits, err := f.repo.SearchEntries(context.Background(), store.EntrySearch{Query: "workspace-only token", CurrentSessionID: "missing"})
	assert.NoErr(t, err)
	assert.Empty(t, storeHits)
}
