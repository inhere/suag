package axconfig

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
)

func TestValidateConfigCandidateUsesExistingReaders(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	cases := []struct {
		name              string
		content           string
		key               string
		allowMissingModel bool
		wantErr           bool
	}{
		{name: "valid provider", content: "model: gateway/m\nproviders:\n  gateway:\n    type: openai\n", key: "providers.gateway.type"},
		{name: "reader rejects invalid provider", content: "model: gateway/m\nproviders:\n  gateway:\n    type: unsupported\n", key: "providers.gateway.type", wantErr: true},
		{name: "unset main model is an editable state", content: "chat:\n  history_size: 12\n", key: "model", allowMissingModel: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfigCandidate([]byte(tc.content), tc.key, tc.allowMissingModel)
			if tc.wantErr {
				assert.Err(t, err)
				assert.Eq(t, 2, err.(interface{ ExitCode() int }).ExitCode())
				return
			}
			assert.NoErr(t, err)
		})
	}
}
