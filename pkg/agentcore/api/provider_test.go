package api

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestGenerateResultValidate_AcceptsContentOnlyResult(t *testing.T) {
	result := &GenerateResult{
		Content:      "ok",
		FinishReason: FinishFinal,
	}

	assert.NoErr(t, result.Validate())
}
