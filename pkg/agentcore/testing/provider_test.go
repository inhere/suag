package agentcoretesting

import (
	"errors"
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func TestRunProviderSuite_WithMockProvider(t *stdtesting.T) {
	providerErr := errors.New("provider boom")
	streamProviderErr := errors.New("stream provider boom")
	streamHandlerErr := errors.New("stream handler boom")

	RunProviderSuite(t, func(t *stdtesting.T) ProviderFixture {
		t.Helper()

		return NewProviderFixture(
			mock.NewProvider(
				mock.Final("stream done"),
				mock.Error(providerErr),
			),
			NewGenerateCase(
				"final_result",
				&agentcore.GenerateRequest{System: "system"},
				&agentcore.GenerateResult{
					Content:      "stream done",
					FinishReason: agentcore.FinishFinal,
				},
			),
		).WithGenerateError(
			"provider_error",
			&agentcore.GenerateRequest{System: "system"},
			providerErr,
		).WithStreamingProvider(mock.NewProvider(
			mock.Stream(
				mock.Error(streamProviderErr),
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "partial"},
			),
			mock.Stream(
				mock.Final("should not complete"),
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "boom"},
			),
			mock.Stream(
				mock.Final("stream done"),
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "stream "},
				agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "done"},
				agentcore.StreamEvent{Kind: agentcore.StreamEventMessageStop},
			),
		)).WithStreamingError(StreamingCase{
			Name:    "stream_provider_error",
			Request: &agentcore.GenerateRequest{System: "system"},
			WantEvents: []agentcore.StreamEvent{
				{Kind: agentcore.StreamEventTextDelta, Delta: "partial"},
			},
			WantErr: streamProviderErr,
		}).WithStreamingError(StreamingCase{
			Name:    "stream_handler_error",
			Request: &agentcore.GenerateRequest{System: "system"},
			WantEvents: []agentcore.StreamEvent{
				{Kind: agentcore.StreamEventTextDelta, Delta: "boom"},
			},
			WantErr: streamHandlerErr,
			EmitErr: streamHandlerErr,
		}).WithStreaming(StreamingCase{
			Name:    "stream_success",
			Request: &agentcore.GenerateRequest{System: "system"},
			WantEvents: []agentcore.StreamEvent{
				{Kind: agentcore.StreamEventTextDelta, Delta: "stream "},
				{Kind: agentcore.StreamEventTextDelta, Delta: "done"},
				{Kind: agentcore.StreamEventMessageStop},
			},
			WantResult: &agentcore.GenerateResult{
				Content:      "stream done",
				FinishReason: agentcore.FinishFinal,
			},
		})
	})
}
