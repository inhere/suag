package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gookit/config/v2"

	"github.com/inhere/axon/internal/platform/llm"
)

// llmProviderSettings is the normalized provider configuration used by the CLI
// app wiring. It keeps protocol selection and vendor labeling separate so the
// rest of the runtime does not need to understand legacy config fallbacks.
type llmProviderSettings struct {
	APIType      string
	ProviderName string
	LegacyName   string
	Model        string
	APIKey       string
	BaseURL      string
}

func readLLMProviderSettings() llmProviderSettings {
	settings := llmProviderSettings{
		APIType:      normalizeConfigToken(config.String("llm.api_type")),
		ProviderName: normalizeConfigToken(config.String("llm.provider_name")),
		LegacyName:   normalizeConfigToken(config.String("llm.provider", "openai")),
		Model:        strings.TrimSpace(config.String("llm.model")),
		APIKey:       strings.TrimSpace(config.String("llm.api_key")),
		BaseURL:      strings.TrimSpace(config.String("llm.base_url")),
	}
	return normalizeLLMProviderSettings(settings)
}

func normalizeLLMProviderSettings(settings llmProviderSettings) llmProviderSettings {
	normalized := settings

	if normalized.APIType == "" {
		switch normalized.LegacyName {
		case "claude":
			normalized.APIType = "anthropic"
			if normalized.ProviderName == "" {
				normalized.ProviderName = "claude"
			}
		case "deepseek":
			normalized.APIType = "openai"
			if normalized.ProviderName == "" {
				normalized.ProviderName = "deepseek"
			}
		default:
			normalized.APIType = "openai"
			if normalized.ProviderName == "" {
				normalized.ProviderName = firstNonEmpty(normalized.LegacyName, "openai")
			}
		}
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

func buildLLMProviderFromSettings(settings llmProviderSettings) (llm.Provider, error) {
	settings = normalizeLLMProviderSettings(settings)

	switch settings.APIType {
	case "anthropic":
		return llm.NewClaudeProvider(llm.ClaudeConfig{
			APIKey: firstNonEmpty(settings.APIKey, os.Getenv("ANTHROPIC_API_KEY")),
			Model:  settings.Model,
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
				}), nil
			}
			return llm.NewDeepSeekProvider(key, settings.Model), nil
		default:
			key = firstNonEmpty(key, os.Getenv("OPENAI_API_KEY"))
			return llm.NewOpenAIProvider(llm.OpenAIConfig{
				APIKey:  key,
				Model:   settings.Model,
				BaseURL: settings.BaseURL,
				Name:    settings.ProviderName,
			}), nil
		}
	default:
		return nil, fmt.Errorf("unsupported llm.api_type %q", settings.APIType)
	}
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
