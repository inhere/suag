package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal/store"
)

func TestGCReportIncludesSnapshotBytes(t *testing.T) {
	var out bytes.Buffer
	writeGCReport(&out, store.GCReport{SnapshotBytes: 17}, time.Unix(0, 0))
	assert.StrContains(t, out.String(), "snapshots 17 字节")
}
