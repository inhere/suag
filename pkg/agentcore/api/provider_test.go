package api

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) Generate(context.Context, *GenerateRequest) (*GenerateResult, error) {
	return &GenerateResult{
		Content:      "ok",
		FinishReason: FinishFinal,
	}, nil
}

type fakeEmbeddingProvider struct {
	fakeProvider
}

func (fakeEmbeddingProvider) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 2, 3}, nil
}

func TestEmbeddingProvider_IsOptionalExtension(t *testing.T) {
	var provider Provider = fakeProvider{}
	var embedder EmbeddingProvider = fakeEmbeddingProvider{}

	assert.Eq(t, "fake", provider.Name())
	assert.Eq(t, "fake", embedder.Name())
}

func TestGenerateResultValidate_AcceptsContentOnlyResult(t *testing.T) {
	result := &GenerateResult{
		Content:      "ok",
		FinishReason: FinishFinal,
	}

	assert.NoErr(t, result.Validate())
}
