package agentcore

import (
	"github.com/inhere/axon/pkg/agentcore/api"

	internalutils "github.com/inhere/axon/pkg/agentcore/internal/utils"
)

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
