package axconfig

import (
	"fmt"
	"os"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/internal/platform/llm"
)

// LLMProviderSettings is the normalized provider configuration used by app
// wiring. It keeps protocol selection and vendor labeling separate so the rest
// of the runtime can assemble providers from a single v0 config shape.
type LLMProviderSettings struct {
	APIType      string
	ProviderName string
	Model        string
	APIKey       string
	BaseURL      string
	// Headers are extra request headers from llm.headers. A value may use
	// llm.SessionIDPlaceholder ("{{session_id}}") for the current session id.
	Headers map[string]string
}

func (s LLMProviderSettings) LogArgs() []any {
	args := []any{
		"api_type", s.APIType,
		"provider_name", s.ProviderName,
	}
	if s.Model != "" {
		args = append(args, "model", s.Model)
	}
	if s.BaseURL != "" {
		args = append(args, "base_url", s.BaseURL)
	}
	return args
}

func ReadLLMProviderSettings() LLMProviderSettings {
	return ReadLLMProviderSettingsWithOverrides("", "")
}

// ReadLLMProviderSettingsWithOverrides applies CLI flags, environment, then file settings.
func ReadLLMProviderSettingsWithOverrides(modelFlag, providerFlag string) LLMProviderSettings {
	settings := LLMProviderSettings{
		APIType:      normalizeConfigToken(gkconfig.String("llm.api_type")),
		ProviderName: normalizeConfigToken(gkconfig.String("llm.provider_name")),
		Model:        strings.TrimSpace(gkconfig.String("llm.model")),
		APIKey:       strings.TrimSpace(gkconfig.String("llm.api_key")),
		BaseURL:      strings.TrimSpace(gkconfig.String("llm.base_url")),
		Headers:      gkconfig.StringMap("llm.headers"),
	}
	// The provider recorded in the file owns the file's api_key and base_url.
	// Remember it before any override so a different provider never inherits them.
	fileProvider := canonicalProviderName(normalizeLLMProviderSettings(LLMProviderSettings{
		APIType:      settings.APIType,
		ProviderName: settings.ProviderName,
	}).ProviderName)

	// An explicit model (flag or env) is kept across a provider switch; a model
	// that only comes from the file belongs to the file's provider and is dropped.
	explicitModel := firstNonEmpty(modelFlag, os.Getenv("SUAG_MODEL"))
	settings.Model = firstNonEmpty(explicitModel, settings.Model)

	provider := firstNonEmpty(providerFlag, os.Getenv("SUAG_PROVIDER"))
	if provider != "" {
		settings.ProviderName = normalizeConfigToken(provider)
		// A named provider override selects its protocol as well as its label.
		switch settings.ProviderName {
		case "claude", "anthropic":
			settings.APIType = "anthropic"
		case "openai", "deepseek":
			settings.APIType = "openai"
		}

		// Switching to a different provider: the file's credential and endpoint
		// would go to the wrong vendor, so clear them. The new provider's key is
		// then read from its standard env var in BuildLLMProviderFromSettings, and
		// an unset model falls back to that provider's default.
		if canonicalProviderName(settings.ProviderName) != fileProvider {
			settings.APIKey = ""
			settings.BaseURL = ""
			settings.Headers = nil
			if explicitModel == "" {
				settings.Model = ""
			}
		}
	}
	return normalizeLLMProviderSettings(settings)
}

// CompactModelSettings returns the settings used for context summaries: the
// effective settings with Model replaced by llm.compact_model (same provider,
// key and endpoint). ok is false when llm.compact_model is unset or equals the
// current model, in which case the turn's own provider summarizes.
func CompactModelSettings(base LLMProviderSettings) (LLMProviderSettings, bool) {
	model := strings.TrimSpace(gkconfig.String("llm.compact_model"))
	if model == "" || model == base.Model {
		return base, false
	}
	base.Model = model
	return base, true
}

func BuildLLMProvider() (llm.Provider, error) {
	return BuildLLMProviderFromSettings(ReadLLMProviderSettings())
}

func BuildLLMProviderFromSettings(settings LLMProviderSettings) (llm.Provider, error) {
	settings = normalizeLLMProviderSettings(settings)

	switch settings.APIType {
	case "anthropic":
		return llm.NewClaudeProvider(llm.ClaudeConfig{
			APIKey:  firstNonEmpty(settings.APIKey, os.Getenv("ANTHROPIC_API_KEY")),
			Model:   settings.Model,
			Headers: settings.Headers,
		}), nil
	case "openai":
		key := settings.APIKey
		switch settings.ProviderName {
		case "deepseek":
			key = firstNonEmpty(key, os.Getenv("DEEPSEEK_API_KEY"))
			if settings.BaseURL != "" {
				return llm.NewOpenAIProvider(llm.OpenAIConfig{
					APIKey:  key,
					BaseURL: settings.BaseURL,
					Model:   settings.Model,
					Name:    "deepseek",
					Headers: settings.Headers,
				}), nil
			}
			return llm.NewDeepSeekProvider(key, settings.Model, settings.Headers), nil
		default:
			key = firstNonEmpty(key, os.Getenv("OPENAI_API_KEY"))
			return llm.NewOpenAIProvider(llm.OpenAIConfig{
				APIKey:  key,
				Model:   settings.Model,
				BaseURL: settings.BaseURL,
				Name:    settings.ProviderName,
				Headers: settings.Headers,
			}), nil
		}
	default:
		return nil, fmt.Errorf("unsupported llm.api_type %q", settings.APIType)
	}
}

func normalizeLLMProviderSettings(settings LLMProviderSettings) LLMProviderSettings {
	normalized := settings

	if normalized.APIType == "" {
		normalized.APIType = "openai"
	}

	if normalized.ProviderName == "" {
		switch normalized.APIType {
		case "anthropic":
			normalized.ProviderName = "claude"
		default:
			normalized.ProviderName = "openai"
		}
	}

	return normalized
}

// canonicalProviderName maps provider aliases to one vendor, so an alias
// (anthropic) of the file's provider (claude) is not treated as a switch.
func canonicalProviderName(name string) string {
	if name == "anthropic" {
		return "claude"
	}
	return name
}

func normalizeConfigToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
