package axconfig

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

const (
	// ApprovalRulesFileName is the name shared by user and project rule files.
	ApprovalRulesFileName = "approval-rules.yaml"
	// DefaultApprovalRulesPrefixTokens is the number of shell tokens captured by
	// a chat "always allow" answer when the setting is not configured.
	DefaultApprovalRulesPrefixTokens = 2
)

// ApprovalRulesSettings contains the non-rule-file approval settings. Rule
// bodies deliberately live in approval-rules.yaml so persistence can preserve
// the user's comments and layout.
type ApprovalRulesSettings struct {
	Enabled            bool
	AlwaysPrefixTokens int
	PersistAsk         bool
	ProjectTrust       map[string]string
}

// DefaultApprovalRulesSettings returns the secure-by-default rule settings.
func DefaultApprovalRulesSettings() ApprovalRulesSettings {
	return ApprovalRulesSettings{
		Enabled:            true,
		AlwaysPrefixTokens: DefaultApprovalRulesPrefixTokens,
		PersistAsk:         true,
	}
}

// ReadApprovalRulesSettings validates tools.approval_rules. Missing settings
// retain the defaults, while a present value must be an object with only the
// documented fields. This keeps old tools.* keys untouched and fail-closed for
// new configuration mistakes.
func ReadApprovalRulesSettings() (ApprovalRulesSettings, error) {
	settings := DefaultApprovalRulesSettings()
	raw := gkconfig.Get("tools.approval_rules")
	if raw == nil {
		return settings, nil
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return settings, fmt.Errorf("tools.approval_rules must be an object")
	}
	for key := range fields {
		switch key {
		case "enabled", "always_prefix_tokens", "persist_ask", "project_trust":
		default:
			return settings, fmt.Errorf("tools.approval_rules.%s is not a recognized setting", key)
		}
	}
	if value, exists := fields["enabled"]; exists {
		parsed, ok := value.(bool)
		if !ok {
			return settings, fmt.Errorf("tools.approval_rules.enabled must be a boolean")
		}
		settings.Enabled = parsed
	}
	if value, exists := fields["always_prefix_tokens"]; exists {
		parsed, ok := approvalRuleInteger(value)
		if !ok || parsed < 1 {
			return settings, fmt.Errorf("tools.approval_rules.always_prefix_tokens must be a whole number >= 1")
		}
		settings.AlwaysPrefixTokens = parsed
	}
	if value, exists := fields["persist_ask"]; exists {
		parsed, ok := value.(bool)
		if !ok {
			return settings, fmt.Errorf("tools.approval_rules.persist_ask must be a boolean")
		}
		settings.PersistAsk = parsed
	}
	if value, exists := fields["project_trust"]; exists {
		trust, err := parseProjectTrust(value)
		if err != nil {
			return settings, err
		}
		settings.ProjectTrust = trust
	}
	return settings, nil
}

// ResolveUserApprovalRulesPath returns the user-level rule file path.
func ResolveUserApprovalRulesPath() (string, error) {
	home, err := ResolveSugaHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ApprovalRulesFileName), nil
}

// ResolveProjectApprovalRulesPath returns the rule file belonging to workspace.
func ResolveProjectApprovalRulesPath(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		workspace = ResolveWorkspacePath()
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Join(absolute, ".suga", ApprovalRulesFileName), nil
}

var approvalRuleHashPattern = regexp.MustCompile(`^[[:xdigit:]]{64}$`)

func parseProjectTrust(raw any) (map[string]string, error) {
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tools.approval_rules.project_trust must be a map of absolute workspace paths to SHA-256 hashes")
	}
	trust := make(map[string]string, len(fields))
	for workspace, value := range fields {
		absolute, err := filepath.Abs(strings.TrimSpace(workspace))
		if err != nil || !filepath.IsAbs(strings.TrimSpace(workspace)) || strings.TrimSpace(workspace) == "" {
			return nil, fmt.Errorf("tools.approval_rules.project_trust workspace %q must be an absolute path", workspace)
		}
		hash, ok := value.(string)
		hash = strings.TrimSpace(hash)
		if !ok || !approvalRuleHashPattern.MatchString(hash) {
			return nil, fmt.Errorf("tools.approval_rules.project_trust[%q] must be a SHA-256 hash", workspace)
		}
		trust[absolute] = strings.ToLower(hash)
	}
	return trust, nil
}

func approvalRuleInteger(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		return int(value), int64(int(value)) == value
	case uint:
		return int(value), uint(int(value)) == value
	case uint8:
		return int(value), true
	case uint16:
		return int(value), true
	case uint32:
		return int(value), uint32(int(value)) == value
	case uint64:
		return int(value), uint64(int(value)) == value
	case float64:
		return int(value), value == float64(int(value))
	default:
		return 0, false
	}
}
