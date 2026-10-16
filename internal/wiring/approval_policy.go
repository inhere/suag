package wiring

import (
	"github.com/inhere/suga/pkg/agentcore/tools"
	approvalrules "github.com/inhere/suga/pkg/agentcore/tools/rules"
)

// approvalDefaults holds the fallback action for each kind of tool. Rules are
// evaluated before these defaults; keeping the categories here prevents a
// native read-only or agent-state tool from being confused with an MCP tool.
type approvalDefaults struct {
	Native     approvalrules.Action
	MCP        approvalrules.Action
	ReadOnly   approvalrules.Action
	AgentState approvalrules.Action
}

// approvalPolicy is the entry-point snapshot. Legacy MCP rules are included
// only by the CLI compiler; ACP receives a separate snapshot without them.
type approvalPolicy struct {
	Mode     approvalrules.Mode
	ReadOnly bool
	Rules    []approvalrules.Rule
	Defaults approvalDefaults
}

// defaultApprovalDefaults returns the defaults fixed by the approval design.
// A read-only or agent-state tool is still allow-by-default, but an explicit
// rule can change that result before this fallback is consulted.
func defaultApprovalDefaults(mode approvalrules.Mode, chatAuto bool) approvalDefaults {
	defaults := approvalDefaults{
		Native:     approvalrules.Allow,
		MCP:        approvalrules.Deny,
		ReadOnly:   approvalrules.Allow,
		AgentState: approvalrules.Allow,
	}
	switch mode {
	case approvalrules.ModeChat:
		defaults.Native = approvalrules.Ask
		if chatAuto {
			defaults.Native = approvalrules.Allow
		}
	case approvalrules.ModeACP:
		defaults.Native = approvalrules.Ask
		defaults.MCP = approvalrules.Ask
	case approvalrules.ModeRun:
		// run keeps its existing non-interactive default: native tools run and
		// MCP tools remain denied unless a legacy allowlist entry matches.
	}
	return defaults
}

// compileApprovalPolicy merges file rules with the supplied entry-point rules.
// The caller supplies legacy rules only for chat/run; this deliberate seam is
// what keeps tools.auto_allow and tools.read_only_allow out of ACP.
func compileApprovalPolicy(mode approvalrules.Mode, chatAuto, readOnly bool, loaded ApprovalRules, legacy []approvalrules.Rule) approvalPolicy {
	compiled := approvalPolicy{Mode: mode, ReadOnly: readOnly, Defaults: defaultApprovalDefaults(mode, chatAuto)}
	if loaded.Settings.Enabled {
		compiled.Rules = append(compiled.Rules, loaded.EffectiveRules()...)
	}
	compiled.Rules = append(compiled.Rules, legacy...)
	return compiled
}

// compileCLIApprovalPolicy loads user/project rules and compiles the legacy MCP
// allowlist for exactly the chat/run entry points.
func compileCLIApprovalPolicy(workspace string, mode approvalrules.Mode, chatAuto, readOnly bool) (approvalPolicy, error) {
	loaded, err := LoadApprovalRules(workspace)
	if err != nil {
		return approvalPolicy{}, err
	}
	legacy, err := compileLegacyMCPRules(readOnly)
	if err != nil {
		return approvalPolicy{}, err
	}
	return compileApprovalPolicy(mode, chatAuto, readOnly, loaded, legacy), nil
}

// compileACPApprovalPolicy intentionally has no legacy parameter. ACP's MCP
// default is ask (or deny in read-only mode at its own entry gate), independent
// of the CLI's auto_allow/read_only_allow configuration.
func compileACPApprovalPolicy(workspace string, readOnly bool) (approvalPolicy, error) {
	loaded, err := LoadApprovalRules(workspace)
	if err != nil {
		return approvalPolicy{}, err
	}
	return compileApprovalPolicy(approvalrules.ModeACP, false, readOnly, loaded, nil), nil
}

// Decide applies explicit rules first and then the category-specific default.
// An ACP/run/chat caller can translate Ask into its own interaction mechanism;
// this layer only computes the stable policy result.
func (p approvalPolicy) Decide(target approvalrules.Target, meta tools.Meta) (approvalrules.Action, *approvalrules.Rule) {
	if target.Mode == "" {
		target.Mode = p.Mode
	}
	defaultAction := p.Defaults.Native
	switch {
	case target.MCP:
		defaultAction = p.Defaults.MCP
	case meta.AgentState:
		defaultAction = p.Defaults.AgentState
	case meta.ReadOnly:
		defaultAction = p.Defaults.ReadOnly
	}
	action, hit := (approvalrules.Policy{Rules: p.Rules, Default: defaultAction}).Decide(target)
	if p.ReadOnly && target.MCP && (hit == nil || hit.Source != approvalrules.SourceLegacy) {
		// --read-only keeps its old MCP boundary: only the explicitly configured
		// read_only_allow legacy entries can grant an MCP call.
		return approvalrules.Deny, hit
	}
	return action, hit
}
