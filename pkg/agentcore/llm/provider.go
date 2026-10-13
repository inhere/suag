// Package llm implements first-party model providers for Suga.
package llm

import agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"

type Provider = agentcoreapi.Provider
type StreamingProvider = agentcoreapi.StreamingProvider
type GenerateRequest = agentcoreapi.GenerateRequest
type GenerateResult = agentcoreapi.GenerateResult
type FinishReason = agentcoreapi.FinishReason

const (
	FinishFinal     = agentcoreapi.FinishFinal
	FinishToolCalls = agentcoreapi.FinishToolCalls
)
