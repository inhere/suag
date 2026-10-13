package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

// envAsMap indexes a child environment by upper-case variable name.
func envAsMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[strings.ToUpper(k)] = v
	}
	return m
}

// TestIsolatedEnvDropsHostAgentVars: a task's child process must not inherit
// the agent variables of the machine running the eval, or it would read a real
// home/config instead of the task's temp SUGA_HOME.
func TestIsolatedEnvDropsHostAgentVars(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	t.Run("SUGA_CONFIG from the host is dropped", func(t *testing.T) {
		t.Setenv("SUGA_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

		got := envAsMap(isolatedEnv(home))

		_, hasConfig := got["SUGA_CONFIG"]
		assert.False(t, hasConfig)
		assert.Eq(t, home, got["SUGA_HOME"])
	})

	t.Run("SUGA_HOME and SUGA_MODEL from the host are dropped", func(t *testing.T) {
		t.Setenv("SUGA_HOME", filepath.Join(t.TempDir(), "host-home"))
		t.Setenv("SUGA_MODEL", "host/model")

		got := envAsMap(isolatedEnv(home))

		assert.Eq(t, home, got["SUGA_HOME"])
		_, hasModel := got["SUGA_MODEL"]
		assert.False(t, hasModel)
	})
}
