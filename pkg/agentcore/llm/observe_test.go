package llm

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

type metadataProvider struct{}

func (metadataProvider) Name() string { return "metadata-provider" }
func (metadataProvider) Generate(context.Context, *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	return &agentcoreapi.GenerateResult{
		Content:      "ok",
		FinishReason: agentcoreapi.FinishFinal,
		Usage:        agentcoreapi.Usage{InputTokens: 4, OutputTokens: 2},
		Metadata: map[string]any{
			agentcoreapi.UsageEstimatedMetaKey:        true,
			agentcoreapi.EstimatedInputTokensMetaKey:  5,
			agentcoreapi.EstimatedOutputTokensMetaKey: 3,
		},
	}, nil
}

func TestGenerateObservedMapsEstimatedMetadataToAttempt(t *testing.T) {
	var got Attempt
	_, err := GenerateObserved(context.Background(), metadataProvider{}, &agentcoreapi.GenerateRequest{}, 1,
		func(_ context.Context, attempt Attempt) { got = attempt })
	assert.NoErr(t, err)
	assert.True(t, got.Estimated)
	assert.Eq(t, 5, got.EstimatedInputTokens)
	assert.Eq(t, 3, got.EstimatedOutputTokens)
	assert.Eq(t, 4, got.Usage.InputTokens)
	assert.Eq(t, 2, got.Usage.OutputTokens)
}

func TestGenerateObservedWithoutEstimatedMetadataKeepsFalse(t *testing.T) {
	var got Attempt
	provider := metadataProvider{}
	_, err := GenerateObserved(context.Background(), provider, &agentcoreapi.GenerateRequest{}, 1,
		func(_ context.Context, attempt Attempt) { got = attempt })
	assert.NoErr(t, err)
	// The provider above is deliberately metadata-bearing; this assertion keeps
	// the false zero-value contract covered by a result without those keys below.
	got = Attempt{}
	plain := plainMetadataProvider{}
	_, err = GenerateObserved(context.Background(), plain, &agentcoreapi.GenerateRequest{}, 1,
		func(_ context.Context, attempt Attempt) { got = attempt })
	assert.NoErr(t, err)
	assert.False(t, got.Estimated)
	assert.Eq(t, 0, got.EstimatedInputTokens)
	assert.Eq(t, 0, got.EstimatedOutputTokens)
}

type plainMetadataProvider struct{}

func (plainMetadataProvider) Name() string { return "plain-metadata-provider" }
func (plainMetadataProvider) Generate(context.Context, *agentcoreapi.GenerateRequest) (*agentcoreapi.GenerateResult, error) {
	return &agentcoreapi.GenerateResult{Content: "ok", FinishReason: agentcoreapi.FinishFinal}, nil
}
