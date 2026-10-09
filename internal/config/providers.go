package axconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// Provider protocol types accepted in providers.<id>.type.
const (
	ProviderTypeOpenAI    = "openai"
	ProviderTypeAnthropic = "anthropic"
)

// ModelConfig is the per-model override under providers.<id>.models.<model>.
type ModelConfig struct {
	ContextWindow int
}

// ProviderConfig is one resolved providers.<id> entry (preset merged with user
// config). Headers values may contain llm.SessionIDPlaceholder, kept verbatim.
type ProviderConfig struct {
	ID      string
	Type    string // openai | anthropic
	BaseURL string
	APIKey  string
	Headers map[string]string
	Models  map[string]ModelConfig
}

// ModelSpec is a model bound to the provider that serves it.
type ModelSpec struct {
	Provider ProviderConfig
	Model    string
}

// Ref returns the canonical "provider/model" form.
func (s ModelSpec) Ref() string { return s.Provider.ID + "/" + s.Model }

// ModelSettings is the effective model selection: main, optional compact
// (summary) model and ordered fallbacks.
type ModelSettings struct {
	Main      ModelSpec
	Compact   *ModelSpec
	Fallbacks []ModelSpec
}

// presetProviders are the built-in provider ids. api_key comes from KeyEnv.
var presetProviders = map[string]struct {
	Type, BaseURL, KeyEnv string
}{
	"openai":    {ProviderTypeOpenAI, "https://api.openai.com/v1", "OPENAI_API_KEY"},
	"anthropic": {ProviderTypeAnthropic, "https://api.anthropic.com", "ANTHROPIC_API_KEY"},
	"deepseek":  {ProviderTypeOpenAI, "https://api.deepseek.com/v1", "DEEPSEEK_API_KEY"},
}

// ReadProviders returns every defined provider: the built-in presets, each
// overridden field by field by the same-named providers.<id> entry (empty
// fields never override), plus user-defined providers, which must set type.
func ReadProviders() (map[string]ProviderConfig, error) {
	providers := make(map[string]ProviderConfig, len(presetProviders))
	for id, p := range presetProviders {
		providers[id] = ProviderConfig{
			ID:      id,
			Type:    p.Type,
			BaseURL: p.BaseURL,
			APIKey:  strings.TrimSpace(os.Getenv(p.KeyEnv)),
		}
	}

	raw, _ := gkconfig.Get("providers").(map[string]any)
	for id, item := range raw {
		id = strings.TrimSpace(id)
		entry, _ := item.(map[string]any)
		user := ProviderConfig{
			ID:      id,
			Type:    normalizeConfigToken(stringValue(entry["type"])),
			BaseURL: strings.TrimSpace(stringValue(entry["base_url"])),
			APIKey:  strings.TrimSpace(stringValue(entry["api_key"])),
			Headers: normalizeStringMap(entry["headers"]),
			Models:  readModelConfigs(entry["models"]),
		}

		merged, isPreset := providers[id]
		if !isPreset {
			merged = ProviderConfig{ID: id}
		}
		merged.Type = firstNonEmpty(user.Type, merged.Type)
		merged.BaseURL = firstNonEmpty(user.BaseURL, merged.BaseURL)
		merged.APIKey = firstNonEmpty(user.APIKey, merged.APIKey)
		if user.Headers != nil {
			merged.Headers = user.Headers
		}
		if user.Models != nil {
			merged.Models = user.Models
		}
		if merged.Type == "" {
			return nil, fmt.Errorf("providers.%s: type is required for a non-preset provider (openai|anthropic)", id)
		}
		if merged.Type != ProviderTypeOpenAI && merged.Type != ProviderTypeAnthropic {
			return nil, fmt.Errorf("providers.%s: unsupported type %q (want openai|anthropic)", id, merged.Type)
		}
		providers[id] = merged
	}
	return providers, nil
}

// readModelConfigs parses providers.<id>.models. It reads the raw map rather
// than a dotted key path because model ids may contain dots (e.g. gpt-4.1).
func readModelConfigs(raw any) map[string]ModelConfig {
	items, ok := raw.(map[string]any)
	if !ok || len(items) == 0 {
		return nil
	}
	models := make(map[string]ModelConfig, len(items))
	for name, item := range items {
		var cfg ModelConfig
		if entry, ok := item.(map[string]any); ok {
			cfg.ContextWindow = intValue(entry["context_window"])
		}
		models[strings.TrimSpace(name)] = cfg
	}
	return models
}

func intValue(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// ResolveModel parses "provider/model" or a bare model id. It splits only at
// the first "/" and only when the prefix is a defined provider; otherwise the
// whole string is the model id (e.g. "meta/llama-3") served by defaultProvider.
func ResolveModel(ref string, providers map[string]ProviderConfig, defaultProvider string) (ModelSpec, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ModelSpec{}, errors.New("model reference is empty")
	}
	providerID, model := defaultProvider, ref
	if prefix, rest, found := strings.Cut(ref, "/"); found {
		if _, defined := providers[prefix]; defined {
			providerID, model = prefix, rest
		}
	}
	if model == "" {
		return ModelSpec{}, fmt.Errorf("model reference %q has no model id", ref)
	}
	if providerID == "" {
		return ModelSpec{}, fmt.Errorf("model %q has no provider; use <provider>/<model>", ref)
	}
	provider, ok := providers[providerID]
	if !ok {
		return ModelSpec{}, fmt.Errorf("provider %q is not defined (presets: openai, anthropic, deepseek; add it under providers:)", providerID)
	}
	return ModelSpec{Provider: provider, Model: model}, nil
}

// CheckLegacyConfig rejects the removed llm: section and SUAG_PROVIDER.
func CheckLegacyConfig() error {
	if gkconfig.Get("llm") != nil {
		return errors.New("llm: section is no longer supported; use `model: <provider>/<model>` and `providers:` (see docs/usage/llm-config-v0.md)")
	}
	if strings.TrimSpace(os.Getenv("SUAG_PROVIDER")) != "" {
		return errors.New("SUAG_PROVIDER is no longer supported; use SUAG_MODEL=<provider>/<model> (or --model <provider>/<model>)")
	}
	return nil
}

// ReadModelSettings resolves the main model (flagModel > SUAG_MODEL > model),
// compact_model and fallback_models. The main ref's provider is the default
// provider for bare model ids in compact_model and fallback_models.
func ReadModelSettings(flagModel string) (ModelSettings, error) {
	if err := CheckLegacyConfig(); err != nil {
		return ModelSettings{}, err
	}
	providers, err := ReadProviders()
	if err != nil {
		return ModelSettings{}, err
	}

	mainRef := firstNonEmpty(flagModel, os.Getenv("SUAG_MODEL"), gkconfig.String("model"))
	if strings.TrimSpace(mainRef) == "" {
		return ModelSettings{}, errors.New("model is not configured; set `model: <provider>/<model>` (or SUAG_MODEL / --model)")
	}
	// A bare main model has no provider to default to, so ResolveModel errors.
	main, err := ResolveModel(mainRef, providers, "")
	if err != nil {
		return ModelSettings{}, fmt.Errorf("model: %w", err)
	}
	settings := ModelSettings{Main: main}

	if ref := strings.TrimSpace(gkconfig.String("compact_model")); ref != "" {
		spec, err := ResolveModel(ref, providers, main.Provider.ID)
		if err != nil {
			return ModelSettings{}, fmt.Errorf("compact_model: %w", err)
		}
		settings.Compact = &spec
	}
	for i, ref := range normalizeStringList(gkconfig.Get("fallback_models")) {
		spec, err := ResolveModel(ref, providers, main.Provider.ID)
		if err != nil {
			return ModelSettings{}, fmt.Errorf("fallback_models[%d]: %w", i, err)
		}
		settings.Fallbacks = append(settings.Fallbacks, spec)
	}
	return settings, nil
}

// ContextWindowFor returns providers.<id>.models.<model>.context_window when
// set, otherwise the window inferred from the model name.
func ContextWindowFor(spec ModelSpec) int {
	if cfg, ok := spec.Provider.Models[spec.Model]; ok && cfg.ContextWindow > 0 {
		return cfg.ContextWindow
	}
	return ContextWindowForModel(spec.Model)
}
