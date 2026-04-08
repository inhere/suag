package agentcore

import "github.com/inhere/axon/pkg/agentcore/api"

type Provider = api.Provider
type StreamEventKind = api.StreamEventKind

const (
	StreamEventTextDelta   = api.StreamEventTextDelta
	StreamEventMessageStop = api.StreamEventMessageStop
)

type StreamEvent = api.StreamEvent
type StreamFunc = api.StreamFunc
type StreamingProvider = api.StreamingProvider
type GenerateRequest = api.GenerateRequest
type GenerateResult = api.GenerateResult
type FinishReason = api.FinishReason

const (
	FinishFinal     = api.FinishFinal
	FinishToolCalls = api.FinishToolCalls
)
