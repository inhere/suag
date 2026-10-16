package wiring

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	axconfig "github.com/inhere/suga/internal/config"
	"github.com/inhere/suga/pkg/agentcore/tools/rules"
)

const (
	// maxApprovalRulesFileBytes bounds parsing work and keeps rule reloads small.
	maxApprovalRulesFileBytes = 64 * 1024
	// maxApprovalRules prevents a valid but unexpectedly large policy from
	// making every tool decision expensive. It is independent of the byte cap.
	maxApprovalRules = 256
)

// ApprovalRuleSource is one parsed rule file. The AST and original bytes are
// retained for the later read-modify-write task; parsing itself never rewrites
// a user's file or expands YAML aliases.
type ApprovalRuleSource struct {
	Path    string
	Source  rules.Source
	Content []byte
	AST     *ast.File
	Rules   []rules.Rule
}

// ApprovalRules is the validated user/project rule snapshot used by later
// wiring tasks. Disabled settings return an empty snapshot without reading
// either rule file, preserving the legacy approval path.
type ApprovalRules struct {
	Settings axconfig.ApprovalRulesSettings
	User     ApprovalRuleSource
	Project  ApprovalRuleSource
}

// LoadApprovalRules loads and validates the two independent rule files for a
// workspace. A missing file means an empty source; an existing malformed file
// is an error because approval configuration is security-sensitive.
func LoadApprovalRules(workspace string) (ApprovalRules, error) {
	settings, err := axconfig.ReadApprovalRulesSettings()
	if err != nil {
		return ApprovalRules{}, err
	}
	userPath, err := axconfig.ResolveUserApprovalRulesPath()
	if err != nil {
		return ApprovalRules{}, fmt.Errorf("resolve user approval rules: %w", err)
	}
	projectPath, err := axconfig.ResolveProjectApprovalRulesPath(workspace)
	if err != nil {
		return ApprovalRules{}, fmt.Errorf("resolve project approval rules: %w", err)
	}
	snapshot := ApprovalRules{
		Settings: settings,
		User:     ApprovalRuleSource{Path: userPath, Source: rules.SourceUser},
		Project:  ApprovalRuleSource{Path: projectPath, Source: rules.SourceProject},
	}
	if !settings.Enabled {
		return snapshot, nil
	}
	root := workspace
	if strings.TrimSpace(root) == "" {
		root = axconfig.ResolveWorkspacePath()
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return ApprovalRules{}, fmt.Errorf("resolve approval rules root: %w", err)
	}
	for _, source := range []struct {
		path   string
		source rules.Source
		assign func(ApprovalRuleSource)
	}{
		{userPath, rules.SourceUser, func(value ApprovalRuleSource) { snapshot.User = value }},
		{projectPath, rules.SourceProject, func(value ApprovalRuleSource) { snapshot.Project = value }},
	} {
		loaded, err := loadApprovalRuleSource(source.path, source.source, root)
		if err != nil {
			return ApprovalRules{}, err
		}
		source.assign(loaded)
	}
	return snapshot, nil
}

func loadApprovalRuleSource(path string, source rules.Source, root string) (ApprovalRuleSource, error) {
	loaded := ApprovalRuleSource{Path: path, Source: source}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return loaded, nil
	}
	if err != nil {
		return loaded, fmt.Errorf("read approval rules %q: %w", path, err)
	}
	parsed, err := parseApprovalRuleFile(content, path, source, root)
	if err != nil {
		return loaded, err
	}
	parsed.Path = path
	return parsed, nil
}

func parseApprovalRuleFile(content []byte, filename string, source rules.Source, root string) (ApprovalRuleSource, error) {
	loaded := ApprovalRuleSource{Path: filename, Source: source, Content: append([]byte(nil), content...)}
	if len(content) > maxApprovalRulesFileBytes {
		return loaded, fmt.Errorf("approval rules %q exceeds %d bytes", filename, maxApprovalRulesFileBytes)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return loaded, nil
	}
	file, err := parser.ParseBytes(content, parser.ParseComments)
	if err != nil {
		return loaded, fmt.Errorf("parse approval rules %q: %w", filename, err)
	}
	loaded.AST = file
	if len(ast.FilterFile(ast.AnchorType, file)) > 0 {
		return loaded, fmt.Errorf("approval rules %q must not contain YAML anchors", filename)
	}
	if len(ast.FilterFile(ast.AliasType, file)) > 0 {
		return loaded, fmt.Errorf("approval rules %q must not contain YAML aliases", filename)
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return loaded, fmt.Errorf("approval rules %q must contain exactly one YAML document", filename)
	}
	rootNode, ok := file.Docs[0].Body.(*ast.MappingNode)
	if !ok {
		return loaded, fmt.Errorf("approval rules %q must contain a mapping", filename)
	}
	rootFields, err := mappingFields(rootNode, "approval rules")
	if err != nil {
		return loaded, fmt.Errorf("approval rules %q: %w", filename, err)
	}
	rulesNode, ok := rootFields["rules"]
	if !ok {
		return loaded, fmt.Errorf("approval rules %q requires rules", filename)
	}
	sequence, ok := rulesNode.(*ast.SequenceNode)
	if !ok {
		return loaded, fmt.Errorf("approval rules %q: rules must be a list", filename)
	}
	if len(sequence.Values) > maxApprovalRules {
		return loaded, fmt.Errorf("approval rules %q contains more than %d rules", filename, maxApprovalRules)
	}
	for index, node := range sequence.Values {
		rule, err := parseApprovalRuleNode(node, source, index+1, root)
		if err != nil {
			return loaded, fmt.Errorf("approval rules %q rule %d: %w", filename, index+1, err)
		}
		loaded.Rules = append(loaded.Rules, rule)
	}
	return loaded, nil
}

func parseApprovalRuleNode(node ast.Node, source rules.Source, index int, root string) (rules.Rule, error) {
	mapping, ok := node.(*ast.MappingNode)
	if !ok {
		return rules.Rule{}, fmt.Errorf("rule must be a mapping")
	}
	fields, err := mappingFields(mapping, "rule")
	if err != nil {
		return rules.Rule{}, err
	}
	if _, ok := fields["action"]; !ok {
		return rules.Rule{}, fmt.Errorf("action is required")
	}
	action, err := scalarString(fields["action"], "action")
	if err != nil {
		return rules.Rule{}, err
	}
	tool := ""
	if node, ok := fields["tool"]; ok {
		tool, err = scalarString(node, "tool")
		if err != nil {
			return rules.Rule{}, err
		}
		if strings.TrimSpace(tool) == "" {
			return rules.Rule{}, fmt.Errorf("tool must not be empty")
		}
	}
	command, err := stringList(fields["command"], "command")
	if err != nil {
		return rules.Rule{}, err
	}
	pathPatterns, err := stringList(fields["path"], "path")
	if err != nil {
		return rules.Rule{}, err
	}
	if modesNode, ok := fields["modes"]; ok && modesNode.Type() != ast.SequenceType {
		return rules.Rule{}, fmt.Errorf("modes must be a list of strings")
	}
	modes, err := stringList(fields["modes"], "modes")
	if err != nil {
		return rules.Rule{}, err
	}
	if len(command) > 0 && len(pathPatterns) > 0 {
		return rules.Rule{}, fmt.Errorf("command and path are mutually exclusive")
	}
	if len(command) > 0 && tool != "shell_exec" {
		return rules.Rule{}, fmt.Errorf("command rules require tool shell_exec")
	}
	if len(pathPatterns) > 0 && !isExactFileTool(tool) {
		return rules.Rule{}, fmt.Errorf("path rules require an exact file_write, file_edit, or file_remove tool")
	}
	if strings.Contains(tool, "/") && strings.Count(tool, "/") != 1 {
		return rules.Rule{}, fmt.Errorf("MCP tool pattern must be server/tool")
	}
	convertedModes := make([]rules.Mode, len(modes))
	for i, mode := range modes {
		convertedModes[i] = rules.Mode(mode)
	}
	converted := rules.Rule{
		Source:  source,
		Index:   index,
		Tool:    tool,
		Command: command,
		Path:    pathPatterns,
		Root:    root,
		Modes:   convertedModes,
		Action:  rules.Action(action),
	}
	if err := converted.Validate(); err != nil {
		return rules.Rule{}, err
	}
	return converted, nil
}

func mappingFields(mapping *ast.MappingNode, context string) (map[string]ast.Node, error) {
	fields := make(map[string]ast.Node, len(mapping.Values))
	for _, value := range mapping.Values {
		key, err := scalarString(value.Key, context+" key")
		if err != nil {
			return nil, err
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		if context == "approval rules" {
			if key != "rules" {
				return nil, fmt.Errorf("unknown field %q", key)
			}
		} else {
			switch key {
			case "tool", "command", "path", "action", "modes":
			default:
				return nil, fmt.Errorf("unknown field %q", key)
			}
		}
		fields[key] = value.Value
	}
	return fields, nil
}

func scalarString(node ast.Node, field string) (string, error) {
	scalar, ok := node.(ast.ScalarNode)
	if !ok || node.Type() != ast.StringType {
		return "", fmt.Errorf("%s must be a string", field)
	}
	value, ok := scalar.GetValue().(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	return value, nil
}

func stringList(node ast.Node, field string) ([]string, error) {
	if node == nil {
		return nil, nil
	}
	if value, err := scalarString(node, field); err == nil {
		if value == "" {
			return nil, fmt.Errorf("%s must not contain an empty string", field)
		}
		return []string{value}, nil
	}
	sequence, ok := node.(*ast.SequenceNode)
	if !ok {
		return nil, fmt.Errorf("%s must be a string or list of strings", field)
	}
	if len(sequence.Values) == 0 {
		return nil, fmt.Errorf("%s must not be empty", field)
	}
	values := make([]string, 0, len(sequence.Values))
	for _, item := range sequence.Values {
		value, err := scalarString(item, field)
		if err != nil {
			return nil, err
		}
		if value == "" {
			return nil, fmt.Errorf("%s must not contain an empty string", field)
		}
		values = append(values, value)
	}
	return values, nil
}

func isExactFileTool(tool string) bool {
	switch tool {
	case "file_write", "file_edit", "file_remove":
		return true
	default:
		return false
	}
}
