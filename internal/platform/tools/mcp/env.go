package mcp

import "strings"

// credentialEnvNames are the LLM credential variables suag itself reads (see
// internal/config/llm.go). They also match the suffix rule; the explicit list
// keeps them covered if that rule is ever narrowed.
var credentialEnvNames = []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY"}

var credentialEnvSuffixes = []string{"_API_KEY", "_AUTH_TOKEN"}

// isCredentialEnv reports whether an environment variable name looks like a
// credential that an MCP subprocess must not inherit. Names are compared
// case-insensitively because Windows environment names are.
func isCredentialEnv(name string) bool {
	upper := strings.ToUpper(name)
	for _, n := range credentialEnvNames {
		if upper == n {
			return true
		}
	}
	for _, s := range credentialEnvSuffixes {
		if strings.HasSuffix(upper, s) {
			return true
		}
	}
	return false
}

// buildServerEnv returns the MCP subprocess environment: the parent
// environment minus credential variables, then the server's own configured
// entries. Configured entries are passed as-is, so a server may still be given
// a credential explicitly. GOFER_* and everything else is inherited.
func buildServerEnv(parent, configured []string) []string {
	env := make([]string, 0, len(parent)+len(configured))
	for _, kv := range parent {
		name, _, _ := strings.Cut(kv, "=")
		// A leading "=" (Windows drive-cwd entries like "=C:=C:\x") has an empty name; keep it.
		if name != "" && isCredentialEnv(name) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, configured...)
}
