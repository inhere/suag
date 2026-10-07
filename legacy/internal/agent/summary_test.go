package agent

import (
	"strings"
	"testing"
)

func TestExtractSummaryKeepsUTF8Boundaries(t *testing.T) {
	t.Parallel()

	got := extractSummary(strings.Repeat("向工作空间写入内容", 30))

	if strings.Contains(got, "\uFFFD") {
		t.Fatalf("extractSummary() contains replacement rune: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("extractSummary() = %q, want ellipsis suffix", got)
	}
}
