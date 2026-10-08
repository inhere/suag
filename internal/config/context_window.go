package axconfig

import (
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// DefaultContextWindow is used when the model is unknown and llm.context_window
// is unset. 128k is the smallest window among current mainstream chat models,
// so a client computing "used/size" never under-reports pressure by assuming a
// larger window than the model really has.
const DefaultContextWindow = 128000

// knownContextWindows maps lower-case model name fragments to conservative
// context windows. First match wins, so more specific fragments come first.
var knownContextWindows = []struct {
	fragment string
	tokens   int
}{
	{"gpt-4.1", 1000000},
	{"gpt-4o", 128000},
	{"gpt-5", 400000},
	{"deepseek", 128000},
	{"claude", 200000},
}

// ContextWindowForModel returns the window for a model name, or
// DefaultContextWindow when the model is not recognised.
func ContextWindowForModel(model string) int {
	model = strings.ToLower(model)
	for _, known := range knownContextWindows {
		if strings.Contains(model, known.fragment) {
			return known.tokens
		}
	}
	return DefaultContextWindow
}

// ReadContextWindow resolves the model context window: the optional
// llm.context_window setting wins, then the configured model's known window,
// then DefaultContextWindow. It never returns a non-positive value.
func ReadContextWindow() int {
	if configured := gkconfig.Int("llm.context_window"); configured > 0 {
		return configured
	}
	return ContextWindowForModel(ReadLLMProviderSettings().Model)
}
