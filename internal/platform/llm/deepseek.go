package llm

// DeepSeekProvider implements Provider for DeepSeek via OpenAI-compatible API.
type DeepSeekProvider struct {
	*OpenAIProvider
}

// NewDeepSeekProvider creates a DeepSeek provider using the OpenAI-compatible client.
func NewDeepSeekProvider(apiKey, model string, headers map[string]string) *DeepSeekProvider {
	if model == "" {
		model = "deepseek-chat"
	}
	inner := NewOpenAIProvider(OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.deepseek.com/v1",
		Model:   model,
		Name:    "deepseek",
		// DeepSeek doesn't have an embed API; set a placeholder.
		EmbedModel: "deepseek-chat",
		Headers:    headers,
	})
	return &DeepSeekProvider{OpenAIProvider: inner}
}

// Name implements Provider.
func (p *DeepSeekProvider) Name() string { return "deepseek" }
