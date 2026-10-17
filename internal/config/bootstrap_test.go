package axconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func TestEnsureHomeCreatesSafeOnboardingFiles(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("SUGA_HOME", home)

	result, err := EnsureHome(BootstrapOptions{})
	assert.Require(t, assert.NoErr(t, err))
	assert.True(t, result.CreatedHome)
	assert.True(t, result.CreatedConfig)
	assert.True(t, result.CreatedEnv)
	assert.Eq(t, "home", result.Source)
	assertPrivateFile(t, result.Config)
	assertPrivateFile(t, result.Env)

	config, err := os.ReadFile(result.Config)
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, string(config), "model: deepseek/deepseek-chat")
	assert.StrContains(t, string(config), "# providers:")
	env, err := os.ReadFile(result.Env)
	assert.Require(t, assert.NoErr(t, err))
	for _, line := range strings.Split(string(env), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			t.Fatalf("onboarding env contains a value line: %q", line)
		}
	}
}

func TestEnsureHomeDoesNotOverwriteExistingFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	configPath := filepath.Join(home, "config.yaml")
	envPath := filepath.Join(home, ".env")
	config := []byte("# keep this comment\nmodel: openai/old\n")
	env := []byte("OPENAI_API_KEY=keep-me\n")
	assert.NoErr(t, os.WriteFile(configPath, config, 0o600))
	assert.NoErr(t, os.WriteFile(envPath, env, 0o600))

	result, err := EnsureHome(BootstrapOptions{})
	assert.Require(t, assert.NoErr(t, err))
	assert.False(t, result.CreatedConfig)
	assert.False(t, result.CreatedEnv)
	got, err := os.ReadFile(configPath)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, string(config), string(got))
	got, err = os.ReadFile(envPath)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, string(env), string(got))
}

func TestEnsureHomeForceBacksUpConfigButNeverEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	configPath := filepath.Join(home, "config.yaml")
	envPath := filepath.Join(home, ".env")
	oldConfig := []byte("# user config\nmodel: openai/old\n")
	oldEnv := []byte("OPENAI_API_KEY=keep-me\n")
	assert.NoErr(t, os.WriteFile(configPath, oldConfig, 0o600))
	assert.NoErr(t, os.WriteFile(envPath, oldEnv, 0o600))

	result, err := EnsureHome(BootstrapOptions{Force: true})
	assert.Require(t, assert.NoErr(t, err))
	assert.True(t, result.CreatedConfig)
	assert.False(t, result.CreatedEnv)
	assert.True(t, result.Backup != "")
	backup, err := os.ReadFile(result.Backup)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, string(oldConfig), string(backup))
	got, err := os.ReadFile(configPath)
	assert.Require(t, assert.NoErr(t, err))
	assert.StrContains(t, string(got), "model: deepseek/deepseek-chat")
	got, err = os.ReadFile(envPath)
	assert.Require(t, assert.NoErr(t, err))
	assert.Eq(t, string(oldEnv), string(got))
}

func TestEnsureHomeProtectsExplicitConfig(t *testing.T) {
	t.Run("force never overwrites explicit source", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		external := filepath.Join(t.TempDir(), "chosen.yaml")
		t.Setenv("SUGA_HOME", home)
		t.Setenv("SUGA_CONFIG", external)
		assert.NoErr(t, os.WriteFile(external, []byte("model: openai/chosen\n"), 0o600))

		result, err := EnsureHome(BootstrapOptions{Force: true})
		assert.Require(t, assert.Err(t, err))
		var coded interface{ ExitCode() int }
		assert.Require(t, assert.True(t, errors.As(err, &coded)))
		assert.Eq(t, 2, coded.ExitCode())
		assert.Eq(t, "explicit", result.Source)
		_, err = os.Stat(filepath.Join(home, "config.yaml"))
		assert.True(t, errors.Is(err, os.ErrNotExist))
	})

	t.Run("missing explicit source is not silently replaced", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		external := filepath.Join(t.TempDir(), "missing.yaml")
		t.Setenv("SUGA_HOME", home)
		t.Setenv("SUGA_CONFIG", external)

		result, err := EnsureHome(BootstrapOptions{})
		assert.Require(t, assert.Err(t, err))
		assert.Eq(t, "explicit", result.Source)
		_, statErr := os.Stat(filepath.Join(home, ".env"))
		assert.NoErr(t, statErr)
		_, statErr = os.Stat(filepath.Join(home, "config.yaml"))
		assert.True(t, errors.Is(statErr, os.ErrNotExist))
	})
}

func TestEnsureHomeConcurrentFirstRunWritesOnce(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("SUGA_HOME", home)
	var wg sync.WaitGroup
	results := make([]BootstrapResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = EnsureHome(BootstrapOptions{})
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		assert.NoErr(t, err)
	}
	created := 0
	for _, result := range results {
		if result.CreatedConfig {
			created++
		}
	}
	assert.Eq(t, 1, created)
}

func TestEnsureHomeConcurrentAcrossProcessesWritesOnce(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	commands := make([]*exec.Cmd, 0, 2)
	outputs := make([]bytes.Buffer, 2)
	for i := range 2 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestBootstrapHelperProcess$", "--")
		cmd.Stdout = &outputs[i]
		cmd.Stderr = &outputs[i]
		cmd.Env = helperEnv(home)
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		assert.NoErr(t, cmd.Start())
	}
	created := 0
	for i, cmd := range commands {
		err := cmd.Wait()
		output := outputs[i].String()
		assert.NoErr(t, err, output)
		if strings.Contains(output, "created") {
			created++
		}
	}
	assert.Eq(t, 1, created)
}

// TestBootstrapHelperProcess is launched by the cross-process locking test.
func TestBootstrapHelperProcess(t *testing.T) {
	home := os.Getenv("SUGA_BOOTSTRAP_HELPER_HOME")
	if home == "" {
		return
	}
	_ = os.Setenv("SUGA_HOME", home)
	result, err := EnsureHome(BootstrapOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedConfig {
		_, _ = fmt.Fprintln(os.Stdout, "created")
	}
}

func helperEnv(home string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "SUGA_BOOTSTRAP_HELPER_HOME=") || strings.HasPrefix(value, "SUGA_HOME=") || strings.HasPrefix(value, "SUGA_CONFIG=") {
			continue
		}
		env = append(env, value)
	}
	return append(env, "SUGA_BOOTSTRAP_HELPER_HOME="+home, "SUGA_CONFIG=")
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	assert.Require(t, assert.NoErr(t, err))
	if runtime.GOOS != "windows" {
		assert.Eq(t, os.FileMode(0o600), info.Mode().Perm())
	}
}
