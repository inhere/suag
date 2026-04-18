package agentcore

import "github.com/inhere/axon/pkg/agentcore/api"

type ProviderError = api.ProviderError
type ToolError = api.ToolError
type HookError = api.HookError
type StorageError = api.StorageError
type MemoryError = api.MemoryError

var (
	ErrEmptyGenerateResult     = api.ErrEmptyGenerateResult
	ErrMixedGenerateResult     = api.ErrMixedGenerateResult
	ErrInvalidFinishReason     = api.ErrInvalidFinishReason
	ErrMissingToolCallID       = api.ErrMissingToolCallID
	ErrMissingToolName         = api.ErrMissingToolName
	ErrToolArgumentsInvalid    = api.ErrToolArgumentsInvalid
	ErrMissingFinishReason     = api.ErrMissingFinishReason
	ErrProviderRequired        = api.ErrProviderRequired
	ErrRunNotResumable         = api.ErrRunNotResumable
	ErrMemoryStateConflict     = api.ErrMemoryStateConflict
	ErrToolCallsNotImplemented = api.ErrToolCallsNotImplemented
)
