package agentcore

import "github.com/inhere/suga/pkg/agentcore/api"

type ProviderError = api.ProviderError
type ToolError = api.ToolError
type HookError = api.HookError
type StorageError = api.StorageError
type HistoryError = api.HistoryError

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
	ErrHistoryStateConflict    = api.ErrHistoryStateConflict
	ErrToolCallsNotImplemented = api.ErrToolCallsNotImplemented
	ErrWrapUpIgnored           = api.ErrWrapUpIgnored
)
