package personal

import (
	"context"
	"errors"
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore"
)

type estimateTestProvider struct {
	result *agentcore.GenerateResult
	ctx    context.Context
	emit   agentcore.StreamFunc
}

type estimateContextKey struct{}

func (*estimateTestProvider) Name() string             { return "estimate-test" }
func (*estimateTestProvider) Model() string            { return "estimate-model" }
func (*estimateTestProvider) SupportsToolChoice() bool { return true }
func (p *estimateTestProvider) Generate(ctx context.Context, _ *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	p.ctx = ctx
	return p.result, nil
}
func (p *estimateTestProvider) Stream(ctx context.Context, _ *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	p.ctx, p.emit = ctx, emit
	if emit != nil {
		if err := emit(ctx, agentcore.StreamEvent{Kind: agentcore.StreamEventTextDelta, Delta: "x"}); err != nil {
			return nil, err
		}
	}
	return p.result, nil
}

type estimateErrorProvider struct{ err error }

func (p estimateErrorProvider) Name() string { return "estimate-error" }
func (p estimateErrorProvider) Generate(context.Context, *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	return nil, p.err
}

func TestEstimatingProviderModesAndMetadata(t *testing.T) {
	req := &agentcore.GenerateRequest{
		System:   "系统",
		Tools:    []agentcore.ToolSchema{{Name: "read", Description: "读取", InputSchema: map[string]any{"type": "object"}}},
		Messages: []agentcore.Message{{Role: "user", Content: "abcd好"}},
	}
	result := func(usage agentcore.Usage) *agentcore.GenerateResult {
		return &agentcore.GenerateResult{Content: "hello", Reasoning: "😀", ToolCalls: []agentcore.ToolCall{{Name: "read", Arguments: map[string]any{"path": "文"}}}, Usage: usage}
	}

	t.Run("auto fills missing fields and keeps provider metadata", func(t *testing.T) {
		meta := map[string]any{"provider_key": "keep"}
		p := &estimateTestProvider{result: result(agentcore.Usage{InputTokens: 7, CostUSD: 0.25})}
		p.result.Metadata = meta
		got, err := NewEstimatingProvider(p, EstimateOptions{Mode: EstimateAuto, Factor: 1}).Generate(context.Background(), req)
		assert.NoErr(t, err)
		wantOut := estimatedOutputTokens(got)
		assert.Eq(t, 7, got.Usage.InputTokens)
		assert.Eq(t, wantOut, got.Usage.OutputTokens)
		assert.Eq(t, 7+wantOut, got.Usage.TotalTokens)
		assert.Eq(t, true, got.Metadata[agentcore.UsageEstimatedMetaKey])
		assert.Eq(t, wantOut, got.Metadata[agentcore.EstimatedOutputTokensMetaKey])
		_, hasInputEstimate := got.Metadata[agentcore.EstimatedInputTokensMetaKey]
		assert.False(t, hasInputEstimate)
		assert.Eq(t, "keep", got.Metadata["provider_key"])
		_, mutated := meta[agentcore.UsageEstimatedMetaKey]
		assert.False(t, mutated)
	})

	t.Run("on records estimates without replacing real usage", func(t *testing.T) {
		p := &estimateTestProvider{result: result(agentcore.Usage{InputTokens: 99, OutputTokens: 88, TotalTokens: 187, CostUSD: 0.5})}
		got, err := NewEstimatingProvider(p, EstimateOptions{Mode: EstimateOn, Factor: 1.5}).Generate(context.Background(), req)
		assert.NoErr(t, err)
		wantIn := int(float64(estimatedInputTokens(req))*1.5 + 0.5)
		wantOut := int(float64(estimatedOutputTokens(got))*1.5 + 0.5)
		assert.Eq(t, 99, got.Usage.InputTokens)
		assert.Eq(t, 88, got.Usage.OutputTokens)
		assert.Eq(t, 187, got.Usage.TotalTokens)
		assert.Eq(t, wantIn, got.Metadata[agentcore.EstimatedInputTokensMetaKey])
		assert.Eq(t, wantOut, got.Metadata[agentcore.EstimatedOutputTokensMetaKey])
		_, marked := got.Metadata[agentcore.UsageEstimatedMetaKey]
		assert.False(t, marked)
		assert.Eq(t, 0.5, got.Usage.CostUSD)
	})

	t.Run("off leaves usage and metadata untouched", func(t *testing.T) {
		meta := map[string]any{"provider_key": "keep"}
		p := &estimateTestProvider{result: result(agentcore.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5})}
		p.result.Metadata = meta
		got, err := NewEstimatingProvider(p, EstimateOptions{Mode: EstimateOff, Factor: 10}).Generate(context.Background(), req)
		assert.NoErr(t, err)
		assert.Eq(t, agentcore.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}, got.Usage)
		assert.Eq(t, meta, got.Metadata)
	})
}

func TestEstimatingProviderPreservesCapabilitiesAndContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), estimateContextKey{}, "ctx")
	p := &estimateTestProvider{result: &agentcore.GenerateResult{Content: "ok"}}
	wrapped := NewEstimatingProvider(p, EstimateOptions{Mode: EstimateAuto, Factor: 1})
	stream, ok := wrapped.(agentcore.StreamingProvider)
	assert.Require(t, assert.True(t, ok))
	model, ok := wrapped.(interface{ Model() string })
	assert.Require(t, assert.True(t, ok))
	chooser, ok := wrapped.(agentcore.ToolChooser)
	assert.Require(t, assert.True(t, ok))
	assert.Eq(t, "estimate-model", model.Model())
	assert.True(t, chooser.SupportsToolChoice())

	var events int
	got, err := stream.Stream(ctx, &agentcore.GenerateRequest{Messages: []agentcore.Message{{Role: "user", Content: "hi"}}}, func(got context.Context, event agentcore.StreamEvent) error {
		assert.Eq(t, ctx, got)
		assert.Eq(t, agentcore.StreamEventTextDelta, event.Kind)
		events++
		return nil
	})
	assert.NoErr(t, err)
	assert.Eq(t, 1, events)
	assert.Eq(t, ctx, p.ctx)
	assert.NotNil(t, p.emit)
	assert.Eq(t, 1, got.Usage.OutputTokens)
}

func TestEstimatingProviderReturnsProviderErrorUnchanged(t *testing.T) {
	errProvider := estimateErrorProvider{err: errors.New("provider identity")}
	_, err := NewEstimatingProvider(errProvider, EstimateOptions{Mode: EstimateAuto}).Generate(context.Background(), &agentcore.GenerateRequest{})
	assert.Eq(t, errProvider.err, err)
}
