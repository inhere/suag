package main

import (
	"testing"

	"github.com/gookit/config/v2"
	"github.com/gookit/goutil/testutil/assert"

	"github.com/inhere/axon/internal/host/intrunner"
	"github.com/inhere/axon/internal/platform/llm"
	"github.com/inhere/axon/internal/platform/tools"
	"github.com/inhere/axon/internal/platform/tracing"
)

func TestBuildSessionRunner_DefaultsToInternalRunnerWithoutACP(t *testing.T) {
	config.Reset()
	t.Cleanup(config.Reset)

	a := &app{
		provider:  llm.NewMockProvider("mock"),
		registry:  tools.NewRegistry(),
		tracer:    tracing.NewInMemoryTracer(nil),
		workspace: t.TempDir(),
	}

	runner, err := buildSessionRunner(a)
	assert.NoErr(t, err)
	assert.NotNil(t, runner)

	_, ok := runner.(*intrunner.SessionRunner)
	assert.True(t, ok)
}
