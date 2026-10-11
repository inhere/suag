package wiring

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/gookit/goutil/x/assert"

	"github.com/inhere/suag/internal/personal"
)

// TestTaintConfigLists pins memory.taint_tools (default none) and
// memory.taint_on_spill (default shell_exec, an empty list turns it off).
func TestTaintConfigLists(t *testing.T) {
	gkconfig.AddDriver(yaml.Driver)
	load := func(t *testing.T, src string) {
		t.Helper()
		resetConfig(t)
		if src != "" {
			assert.Require(t, assert.NoErr(t, gkconfig.LoadStrings(gkconfig.Yaml, src)))
		}
	}
	t.Run("defaults", func(t *testing.T) {
		load(t, "")
		tools, err := toolNameList("memory.taint_tools", nil)
		assert.NoErr(t, err)
		assert.Empty(t, tools)
		spill, err := toolNameList("memory.taint_on_spill", []string{"shell_exec"})
		assert.NoErr(t, err)
		assert.Eq(t, []string{"shell_exec"}, spill)
	})
	t.Run("empty list turns taint_on_spill off", func(t *testing.T) {
		load(t, "memory:\n  taint_on_spill: []\n")
		spill, err := toolNameList("memory.taint_on_spill", []string{"shell_exec"})
		assert.NoErr(t, err)
		assert.NotNil(t, spill)
		assert.Eq(t, 0, len(spill))
	})
	t.Run("configured lists", func(t *testing.T) {
		load(t, "memory:\n  taint_tools: [shell_exec, ' my_tool ']\n  taint_on_spill: [file_list]\n")
		tools, err := toolNameList("memory.taint_tools", nil)
		assert.NoErr(t, err)
		assert.Eq(t, []string{"shell_exec", "my_tool"}, tools)
		spill, err := toolNameList("memory.taint_on_spill", []string{"shell_exec"})
		assert.NoErr(t, err)
		assert.Eq(t, []string{"file_list"}, spill)
	})
	t.Run("invalid values are startup errors", func(t *testing.T) {
		for _, src := range []string{"memory:\n  taint_tools: shell_exec\n", "memory:\n  taint_tools: [1]\n", "memory:\n  taint_tools: ['']\n"} {
			load(t, src)
			_, err := toolNameList("memory.taint_tools", nil)
			assert.Err(t, err, src)
		}
	})
	t.Run("run mode", func(t *testing.T) {
		assert.Eq(t, personal.ModeAllowWrite, runMode(true))
		assert.Eq(t, personal.ModeReadOnly, runMode(false))
	})
}
