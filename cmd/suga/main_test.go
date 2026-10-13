package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/internal/personal"
)

func TestFormatVersionIncludesAvailableBuildMetadata(t *testing.T) {
	assert.Eq(t, "suga dev (dev)", formatVersion("dev", ""))
	assert.Eq(t, "suga 1.2.3 (abc123)", formatVersion("1.2.3", "abc123"))
}

func TestExitReportInterrupted(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	msg, code := exitReport(canceled, fmt.Errorf("run turn: %w", context.Canceled))
	assert.Eq(t, "已中断", msg)
	assert.Eq(t, 130, code)

	// A canceled error without the signal context canceled is reported as is.
	msg, code = exitReport(context.Background(), context.Canceled)
	assert.Eq(t, "context canceled", msg)
	assert.Eq(t, 1, code)

	msg, code = exitReport(canceled, personal.SessionNotFoundError{ID: "x"})
	assert.Eq(t, "session not found: x", msg)
	assert.Eq(t, 4, code)
}
