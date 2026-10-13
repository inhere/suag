package llm

// Official OpenAI endpoint: reasoning models return a summary and, with
// store false, keep their reasoning across tool calls only through the
// encrypted reasoning sent back.
func init() {
	registerCompat(compatAdapter{
		name:  "openai",
		match: func(host, _ string) bool { return host == "api.openai.com" },
		apply: func(c *Compat) {
			c.ReasoningSummary = true
			c.EncryptedReasoning = true
		},
	})
}
