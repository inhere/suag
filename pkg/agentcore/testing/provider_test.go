package agentcoretesting

import (
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/mock"
)

func TestRunProviderSuite_WithMockProvider(t *stdtesting.T) {
	RunProviderSuite(t, func(t *stdtesting.T) ProviderFixture {
		t.Helper()

		return NewProviderFixture(
			mock.NewProvider(
				mock.Stream(
					mock.Final("stream done"),
					agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "stream "},
					agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "done"},
					agentcore.StreamEvent{Kind: agentcore.StreamEventMessageStop},
				),
			),
			NewGenerateCase(
				"final_result",
				&agentcore.GenerateRequest{System: "system"},
				&agentcore.GenerateResult{
					Content:      "stream done",
					FinishReason: agentcore.FinishFinal,
				},
			),
		).WithStreaming(StreamingCase{
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
