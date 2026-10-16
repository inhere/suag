package wiring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/inhere/suga/pkg/agentcore/tools/rules"
)

// ApprovalRulePersistEvent is the stable event shape used by trace adapters.
// Its three fields correspond to the file, action and result attributes in the
// approval_rule_persist event contract.
type ApprovalRulePersistEvent struct {
	File   string
	Action rules.Action
	Result string
}

// ApprovalRulePersistReporter receives a best-effort writeback observation.
type ApprovalRulePersistReporter func(ApprovalRulePersistEvent)

type persistedApprovalRule struct {
	Tool    string   `yaml:"tool,omitempty"`
	Command any      `yaml:"command,omitempty"`
	Path    any      `yaml:"path,omitempty"`
	Action  string   `yaml:"action"`
	Modes   []string `yaml:"modes,omitempty"`
}

// WriteApprovalRule appends a validated rule to a rule file. The operation is
// serialized with a sidecar lock, writes a same-directory temporary file, and
// replaces the destination atomically before validating the bytes read back.
func WriteApprovalRule(path string, rule rules.Rule) error {
	return PersistApprovalRule(path, rule.Source, rule, nil)
}

// PersistApprovalRule is the writeback seam for chat. Project files accept only
// deny rules: a project allow would turn a model-writable file into a privilege
// grant. The reporter is optional so wiring can connect this operation to trace
// without making the rules package depend on personal storage.
func PersistApprovalRule(path string, destination rules.Source, rule rules.Rule, reporter ApprovalRulePersistReporter) error {
	err := persistApprovalRule(path, destination, rule)
	if reporter != nil {
		result := "ok"
		if err != nil {
			result = "error"
		}
		reporter(ApprovalRulePersistEvent{File: path, Action: rule.Action, Result: result})
	}
	return err
}

func persistApprovalRule(path string, destination rules.Source, rule rules.Rule) error {
	if destination == rules.SourceProject && rule.Action == rules.Allow {
		return fmt.Errorf("project approval rules cannot persist allow rules")
	}
	if err := rule.Validate(); err != nil {
		return fmt.Errorf("validate approval rule for writeback: %w", err)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve approval rule writeback path: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create approval rule directory: %w", err)
	}
	lock, err := acquireApprovalRuleLock(path + ".lock")
	if err != nil {
		return fmt.Errorf("lock approval rules %q: %w", path, err)
	}
	defer lock.Close()

	original, mode, err := readApprovalRuleForWrite(path, destination, rule.Root)
	if err != nil {
		return err
	}
	updated, err := appendApprovalRule(original, path, destination, rule)
	if err != nil {
		return err
	}
	if err := writeApprovalRuleAtomically(path, updated, mode); err != nil {
		return err
	}
	readBack, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read approval rules after writeback: %w", err)
	}
	if _, err := parseApprovalRuleFile(readBack, path, destination, rule.Root); err != nil {
		return fmt.Errorf("validate approval rules after writeback: %w", err)
	}
	return nil
}

func readApprovalRuleForWrite(path string, destination rules.Source, root string) ([]byte, os.FileMode, error) {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, 0o600, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read approval rules for writeback: %w", err)
	}
	if _, err := parseApprovalRuleFile(content, path, destination, root); err != nil {
		return nil, 0, fmt.Errorf("validate approval rules before writeback: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("stat approval rules for writeback: %w", err)
	}
	return content, info.Mode().Perm(), nil
}

func appendApprovalRule(content []byte, filename string, source rules.Source, rule rules.Rule) ([]byte, error) {
	if len(strings.TrimSpace(string(content))) == 0 {
		fragment, err := yaml.Marshal(struct {
			Rules []persistedApprovalRule `yaml:"rules"`
		}{Rules: []persistedApprovalRule{persistedRule(rule)}})
		if err != nil {
			return nil, fmt.Errorf("encode approval rule for empty writeback: %w", err)
		}
		return append([]byte("# Approval rules managed by suga.\n"), fragment...), nil
	}
	file, err := parser.ParseBytes(content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse approval rules for writeback: %w", err)
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return nil, fmt.Errorf("approval rules writeback requires one YAML document")
	}
	root, ok := file.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return nil, fmt.Errorf("approval rules writeback requires a mapping")
	}
	fields, err := mappingFields(root, "approval rules")
	if err != nil {
		return nil, fmt.Errorf("approval rules writeback: %w", err)
	}
	rulesNode, ok := fields["rules"].(*ast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("approval rules writeback requires rules list")
	}
	fragment, err := yaml.Marshal(struct {
		Rules []persistedApprovalRule `yaml:"rules"`
	}{Rules: []persistedApprovalRule{persistedRule(rule)}})
	if err != nil {
		return nil, fmt.Errorf("encode approval rule for writeback: %w", err)
	}
	fragmentFile, err := parser.ParseBytes(fragment, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse encoded approval rule: %w", err)
	}
	fragmentRoot := fragmentFile.Docs[0].Body.(*ast.MappingNode)
	fragmentFields, err := mappingFields(fragmentRoot, "approval rules")
	if err != nil {
		return nil, err
	}
	fragmentSequence := fragmentFields["rules"].(*ast.SequenceNode)
	if len(fragmentSequence.Values) != 1 {
		return nil, fmt.Errorf("encoded approval rule did not produce one item")
	}
	value := fragmentSequence.Values[0]
	if rulesNode.IsFlowStyle {
		if mapping, ok := value.(*ast.MappingNode); ok {
			mapping.SetIsFlowStyle(true)
		}
	} else {
		value.AddColumn(rulesNode.Start.Position.Column - fragmentSequence.Start.Position.Column)
	}
	rulesNode.Values = append(rulesNode.Values, value)
	return []byte(file.String()), nil
}

func persistedRule(rule rules.Rule) persistedApprovalRule {
	return persistedApprovalRule{
		Tool:    rule.Tool,
		Command: persistList(rule.Command),
		Path:    persistList(rule.Path),
		Action:  string(rule.Action),
		Modes:   modeStrings(rule.Modes),
	}
}

func persistList(values []string) any {
	if len(values) == 0 {
		return nil
	}
	if len(values) == 1 {
		return values[0]
	}
	return values
}

func modeStrings(modes []rules.Mode) []string {
	out := make([]string, len(modes))
	for i, mode := range modes {
		out[i] = string(mode)
	}
	return out
}

func writeApprovalRuleAtomically(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create approval rules temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("set approval rules temporary mode: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write approval rules temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync approval rules temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close approval rules temporary file: %w", err)
	}
	var renameErr error
	for attempt := 0; attempt < 5; attempt++ {
		renameErr = os.Rename(tmpName, path)
		if renameErr == nil {
			return nil
		}
		if !renameConflict(renameErr) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return fmt.Errorf("atomically replace approval rules: %w", renameErr)
}
