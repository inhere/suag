package wiring

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/goutil/x/assert"
	"github.com/inhere/suga/pkg/agentcore/tools/rules"
)

func TestPersistApprovalRulePreservesCommentsAndValidatesReadback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval-rules.yaml")
	original := "# keep this comment\nrules:\n  - tool: shell_exec\n    command: go test *\n    action: allow\n"
	assert.NoErr(t, os.WriteFile(path, []byte(original), 0o600))
	var events []ApprovalRulePersistEvent
	rule := rules.Rule{Source: rules.SourceSession, Tool: "file_write", Path: []string{"docs/**"}, Root: t.TempDir(), Action: rules.Deny}
	err := PersistApprovalRule(path, rules.SourceUser, rule, func(event ApprovalRulePersistEvent) { events = append(events, event) })
	assert.NoErr(t, err)
	text, err := os.ReadFile(path)
	assert.NoErr(t, err)
	assert.StrContains(t, string(text), "# keep this comment")
	parsed, err := parseApprovalRuleFile(text, path, rules.SourceUser, rule.Root)
	assert.NoErr(t, err)
	assert.Len(t, parsed.Rules, 2)
	assert.Eq(t, rules.Deny, parsed.Rules[1].Action)
	assert.Len(t, events, 1)
	assert.Eq(t, "ok", events[0].Result)
	assert.Eq(t, "approval-rules.yaml", filepath.Base(events[0].File))
}

func TestPersistApprovalRuleCreatesTemplateAndRejectsProjectAllow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "approval-rules.yaml")
	rule := rules.Rule{Source: rules.SourceSession, Tool: "shell_exec", Command: []string{"go test", "go build"}, Action: rules.Allow}
	assert.Err(t, PersistApprovalRule(path, rules.SourceProject, rule, nil))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	assert.NoErr(t, PersistApprovalRule(path, rules.SourceUser, rule, nil))
	text, err := os.ReadFile(path)
	assert.NoErr(t, err)
	assert.StrContains(t, string(text), "Approval rules managed by suga")
	parsed, err := parseApprovalRuleFile(text, path, rules.SourceUser, t.TempDir())
	assert.NoErr(t, err)
	assert.Len(t, parsed.Rules, 1)
	assert.Eq(t, []string{"go test", "go build"}, parsed.Rules[0].Command)
}

func TestPersistApprovalRuleSerializesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval-rules.yaml")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rule := rules.Rule{Source: rules.SourceSession, Tool: "shell_exec", Command: []string{"go", "test", "pkg" + string(rune('a'+i))}, Action: rules.Deny}
			if err := PersistApprovalRule(path, rules.SourceUser, rule, nil); err != nil {
				t.Errorf("persist rule %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	text, err := os.ReadFile(path)
	assert.NoErr(t, err)
	parsed, err := parseApprovalRuleFile(text, path, rules.SourceUser, t.TempDir())
	assert.NoErr(t, err)
	assert.Len(t, parsed.Rules, 8)
}

func TestReloadApprovalRulesTrustAndFailureRetention(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	projectPath := filepath.Join(workspace, ".suga", "approval-rules.yaml")
	assert.NoErr(t, os.MkdirAll(filepath.Dir(projectPath), 0o700))
	denyText := "rules:\n  - tool: file_write\n    path: .env\n    action: deny\n"
	assert.NoErr(t, os.WriteFile(projectPath, []byte(denyText), 0o600))
	first, err := LoadApprovalRules(workspace)
	assert.NoErr(t, err)
	assert.False(t, first.ProjectAllowTrusted())
	assert.Len(t, first.EffectiveRules(), 1)

	allowText := "rules:\n  - tool: file_write\n    path: docs/**\n    action: allow\n"
	assert.NoErr(t, os.WriteFile(projectPath, []byte(allowText), 0o600))
	second, err := ReloadApprovalRules(first, workspace)
	assert.NoErr(t, err)
	assert.Len(t, second.EffectiveRules(), 1)
	assert.Eq(t, rules.Deny, second.EffectiveRules()[0].Action)

	assert.NoErr(t, os.WriteFile(projectPath, []byte("rules: [\n"), 0o600))
	retained, err := ReloadApprovalRules(second, workspace)
	assert.Err(t, err)
	assert.Eq(t, second.Project.Fingerprint.Hash, retained.Project.Fingerprint.Hash)
	assert.Len(t, retained.Events, 1)
	assert.Eq(t, "approval_rule_reload", retained.Events[0].Kind)

	assert.NoErr(t, os.Remove(projectPath))
	retained, err = ReloadApprovalRules(second, workspace)
	assert.Err(t, err)
	assert.Eq(t, second.Project.Fingerprint.Hash, retained.Project.Fingerprint.Hash)
}

func TestReloadApprovalRulesTrustsExactProjectHash(t *testing.T) {
	gkconfig.Reset()
	t.Cleanup(gkconfig.Reset)
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("SUGA_HOME", home)
	projectPath := filepath.Join(workspace, ".suga", "approval-rules.yaml")
	assert.NoErr(t, os.MkdirAll(filepath.Dir(projectPath), 0o700))
	text := "rules:\n  - tool: file_write\n    path: docs/**\n    action: allow\n"
	assert.NoErr(t, os.WriteFile(projectPath, []byte(text), 0o600))
	digest := sha256.Sum256([]byte(text))
	assert.NoErr(t, gkconfig.Set("tools.approval_rules", map[string]any{
		"project_trust": map[string]any{filepath.Clean(workspace): hex.EncodeToString(digest[:])},
	}))
	loaded, err := LoadApprovalRules(workspace)
	assert.NoErr(t, err)
	assert.True(t, loaded.ProjectAllowTrusted())
	assert.Len(t, loaded.EffectiveRules(), 1)
	assert.Eq(t, rules.Allow, loaded.EffectiveRules()[0].Action)
}
