package native

import "context"

// SnapshotKind identifies what the write tool found before it changed a path.
// The strings are persisted by the personal snapshot store, so keep them
// stable across native and host layers.
type SnapshotKind = string

const (
	SnapshotKindFile   SnapshotKind = "file"
	SnapshotKindAbsent SnapshotKind = "absent"
	SnapshotKindTrash  SnapshotKind = "trash"
	SnapshotKindPurged SnapshotKind = "purged"

	SnapshotStatusComplete        = "complete"
	SnapshotStatusIncomplete      = "incomplete"
	SnapshotUnrecoverableOversize = "oversize"
)

// SnapshotRequest carries the path and original bytes to the snapshot owner.
// Root and Rel describe the lexical path used by the tool; RealRel is the
// resolved path below the same root and lets the undo layer re-check links.
type SnapshotRequest struct {
	CallID  string
	Tool    string
	Root    string
	Rel     string
	RealRel string
	Kind    SnapshotKind
	Before  []byte
}

// SnapshotBefore is the non-fatal outcome of creating a before-image. A
// non-empty Unrecoverable value means the write may continue, but the caller
// must tell the model that this call cannot be undone. Other failures are
// returned as errors and must stop the write.
type SnapshotBefore struct {
	Unrecoverable string
	Notice        string
}

// SnapshotResult completes a snapshot after the native write. An incomplete
// result is sent when the write itself fails, so the store does not leave a
// pending record that looks complete.
type SnapshotResult struct {
	CallID        string
	Tool          string
	Root          string
	Rel           string
	RealRel       string
	Kind          SnapshotKind
	AfterSHA256   string
	Size          int64
	Status        string
	Unrecoverable string
	TrashEntries  []string
	PurgedDirs    []string
}

// SnapshotReport is the small, non-sensitive snapshot summary attached to a
// tool event. It deliberately contains no before-image bytes or filesystem
// contents.
type SnapshotReport struct {
	Kind          SnapshotKind
	Status        string
	Bytes         int64
	Unrecoverable string
}

type snapshotReportKey struct{}

// WithSnapshotReport returns a context carrying a fresh report. The observing
// tool layer reads the report after the native tool has finished.
func WithSnapshotReport(ctx context.Context) (context.Context, *SnapshotReport) {
	report := &SnapshotReport{}
	return context.WithValue(ctx, snapshotReportKey{}, report), report
}

// ReportSnapshot replaces the current context report with a snapshot summary.
// Native tools call this only after the snapshot outcome is known; callers
// without an observing context are intentionally ignored.
func ReportSnapshot(ctx context.Context, report SnapshotReport) {
	if current, _ := ctx.Value(snapshotReportKey{}).(*SnapshotReport); current != nil {
		*current = report
	}
}

// Snapshotter is implemented by the session snapshot store. Native tools only
// know this contract; they do not depend on session storage or personal-agent
// packages. A nil Snapshotter disables snapshots and preserves old behavior.
type Snapshotter interface {
	BeforeCall(ctx context.Context, req SnapshotRequest) (SnapshotBefore, error)
	AfterCall(ctx context.Context, result SnapshotResult) error
}
