package agentcore

import (
	"context"

	"github.com/inhere/axon/pkg/agentcore/api"
	"github.com/inhere/axon/pkg/agentcore/files"
	internalutils "github.com/inhere/axon/pkg/agentcore/internal/utils"
)

type ContextBuilder = api.ContextBuilder
type PromptBuilder = api.PromptBuilder

type RunInput = api.RunInput
type RunStatus = api.RunStatus

const (
	RunIdle      = api.RunIdle
	RunRunning   = api.RunRunning
	RunYielded   = api.RunYielded
	RunCompleted = api.RunCompleted
	RunFailed    = api.RunFailed
)

type ResumeMode = api.ResumeMode

const (
	ResumeAuto      = api.ResumeAuto
	ResumeFromState = api.ResumeFromState
	ResumeWithInput = api.ResumeWithInput
)

type Message = api.Message
type Artifact = api.Artifact
type Usage = api.Usage
type Yield = api.Yield
type RunState = api.RunState
type RunResult = api.RunResult

// ---------------- Memory ----------------

type Memory = api.Memory

type noopMemory struct{}

func (noopMemory) Hydrate(context.Context, *RunState) error { return nil }

func (noopMemory) ObserveMessages(context.Context, *RunState, ...Message) error { return nil }

func (noopMemory) BuildContext(_ context.Context, state *RunState) ([]Message, error) {
	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}
	return cloneMessagesValue(state.Messages), nil
}

// ---------------- Storage ----------------

type Storage = api.Storage

// FileStorage is kept as a compatibility alias for the first-party filesystem storage.
type FileStorage = files.Storage

// NewFileStorage is kept as a compatibility constructor for the first-party filesystem storage.
func NewFileStorage(root string) *FileStorage {
	return files.NewStorage(root)
}

// ---------------- Hook ----------------

type Hook = api.Hook
type NopHook = api.NopHook
type Control = api.Control
type RunEvent = api.RunEvent
type PromptEvent = api.PromptEvent
type ContextEvent = api.ContextEvent
type ModelEvent = api.ModelEvent
type ToolEvent = api.ToolEvent
type ErrorEvent = api.ErrorEvent

func NewControl() *Control {
	return api.NewControl()
}

func cloneMetadata(src map[string]any) map[string]any {
	return internalutils.CloneMetadata(src)
}
