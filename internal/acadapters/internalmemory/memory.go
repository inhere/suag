package internalmemory

import (
	"github.com/inhere/axon/internal/platform/memory"
	"github.com/inhere/axon/pkg/agentcore/api"
)

// Adapter keeps the legacy adapter import path alive while delegating to platform memory.
type Adapter = memory.RuntimeMemory

// New adapts an internal memory.Store into agentcore api.Memory.
func New(store memory.Store) api.Memory {
	return memory.NewRuntimeMemory(store)
}
