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

func TestGenerateResultValidate_AcceptsContentOnlyResult(t *testing.T) {
	result := &GenerateResult{
		Content:      "ok",
		FinishReason: FinishFinal,
	}

	assert.NoErr(t, result.Validate())
}
