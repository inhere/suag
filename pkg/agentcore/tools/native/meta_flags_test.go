package native

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/tools"
)

// TestMetaFlags locks which built-ins are External (taint the session) and
// which are Rereadable (self-paginating, never truncated by output wrapping).
func TestMetaFlags(t *testing.T) {
	external := map[string]bool{"web_fetch": true, "web_search": true}
	rereadable := map[string]bool{"file_read": true}
	for _, spec := range Catalog() {
		tool := spec.New(Env{Workspace: t.TempDir(), SerpAPIKey: "test-key"})
		if tool == nil {
			continue
		}
		t.Run(spec.Name, func(t *testing.T) {
			meta := tools.MetaOf(tool)
			assert.Eq(t, external[spec.Name], meta.External, "External")
			assert.Eq(t, rereadable[spec.Name], meta.Rereadable, "Rereadable")
			assert.False(t, meta.AgentState, "AgentState")
		})
	}
	t.Run("skill is not rereadable", func(t *testing.T) {
		assert.False(t, tools.MetaOf(NewSkillTool(nil, 0)).Rereadable)
	})
}
