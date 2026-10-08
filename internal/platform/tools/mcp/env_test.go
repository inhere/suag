package mcp

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestBuildServerEnv(t *testing.T) {
	parent := []string{
		"PATH=/bin", "GOFER_URL=http://x", "GOFER_TOKEN=t", "GOFER_JOB_ID=1",
		"ANTHROPIC_API_KEY=a", "openai_api_key=o", "DEEPSEEK_API_KEY=d",
		"FOO_AUTH_TOKEN=x", "CUSTOM_API_KEY=y", "API_KEY_HINT=keep", "=C:=C:\\x",
	}
	t.Run("strips credentials and keeps the rest", func(t *testing.T) {
		env := buildServerEnv(parent, nil)
		assert.Eq(t, []string{"PATH=/bin", "GOFER_URL=http://x", "GOFER_TOKEN=t", "GOFER_JOB_ID=1", "API_KEY_HINT=keep", "=C:=C:\\x"}, env)
	})
	t.Run("configured env wins and may carry credentials", func(t *testing.T) {
		env := buildServerEnv(parent, []string{"OPENAI_API_KEY=explicit", "PATH=/custom"})
		assert.Eq(t, "OPENAI_API_KEY=explicit", env[len(env)-2])
		assert.Eq(t, "PATH=/custom", env[len(env)-1])
		assert.NotContains(t, env, "ANTHROPIC_API_KEY=a")
	})
}
