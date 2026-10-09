package wiring

import (
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suag/internal/config"
)

func TestContextWindowForSettingsFollowsEffectiveModel(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	_ = gkconfig.Set("llm.api_type", "openai")
	_ = gkconfig.Set("llm.model", "gpt-4o")
	t.Setenv("SUAG_MODEL", "")
	t.Run("file model", func(t *testing.T) {
		assert.Eq(t, 128000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("", "")))
	})
	t.Run("flag overrides file model", func(t *testing.T) {
		assert.Eq(t, 1000000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("gpt-4.1", "")))
	})
	t.Run("env overrides file model", func(t *testing.T) {
		t.Setenv("SUAG_MODEL", "gpt-5")
		assert.Eq(t, 400000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("", "")))
	})
	t.Run("configured context_window wins", func(t *testing.T) {
		_ = gkconfig.Set("llm.context_window", 64000)
		assert.Eq(t, 64000, contextWindowForSettings(appconfig.ReadLLMProviderSettingsWithOverrides("gpt-4.1", "")))
	})
}
