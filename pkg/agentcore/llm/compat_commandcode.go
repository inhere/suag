package llm

// command-code (api.commandcode.ai/provider/v1): an OpenAI-compatible
// gateway serving DeepSeek, GPT, Kimi, ... (Claude over /messages). On the
// Responses API it summarizes and encrypts reasoning, has no named or "auto"
// tool_choice worth sending (oh-my-pi omits it when reasoning), and caps the
// output at 64000 tokens (oh-my-pi's catalog rules for this gateway).
func init() {
	registerCompat(compatAdapter{
		name:  "commandcode",
		match: func(host, _ string) bool { return host == "api.commandcode.ai" },
		apply: func(c *Compat) {
			c.ReasoningSummary = true
			c.EncryptedReasoning = true
			c.NoToolChoice = true
			c.MaxOutputTokens = 64000
		},
	})
}
