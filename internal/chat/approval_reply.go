package chat

import "strings"

// ApprovalReply is the decision made from the one-line answer to an approval
// question. The wiring layer turns the two persistent replies into session rules.
type ApprovalReply uint8

const (
	ReplyDeny ApprovalReply = iota
	ReplyAllow
	ReplyAllowAlways
	ReplyDenyAlways
)

// ParseApprovalReply accepts the stable answer aliases used by chat. Empty and
// unknown answers stay deny, preserving the old fail-closed behavior.
func ParseApprovalReply(answer string) ApprovalReply {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return ReplyAllow
	case "a", "always":
		return ReplyAllowAlways
	case "d", "deny":
		return ReplyDenyAlways
	default:
		return ReplyDeny
	}
}

// PersistReply identifies the requested persistence destination. The wiring layer
// performs the writeback after the session rule has been installed in memory.
type PersistReply uint8

const (
	PersistSession PersistReply = iota
	PersistUser
	PersistProject
)

// ParsePersistReply keeps Enter as the safe default and recognizes only the
// explicit destinations shown in the follow-up question.
func ParsePersistReply(answer string) PersistReply {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "u", "user":
		return PersistUser
	case "p", "project":
		return PersistProject
	default:
		return PersistSession
	}
}

// ApprovalPrompt builds the compact answer line. allowAlways is false for an
// unsafe shell command: a deny scope is still useful, but an allow scope must
// never bypass the command safety matcher.
func ApprovalPrompt(tool, scopeDescription string, allowAlways bool) string {
	prompt := "允许执行 " + tool + "？[y]是 [N]否"
	if allowAlways {
		prompt += " [a]本会话总是允许" + scopeDescription
	}
	prompt += " [d]本会话总是拒绝："
	return prompt
}

// PersistPrompt is explicit about the safe default: an empty answer keeps the
// rule in the current session, while only u/p request a file writeback.
func PersistPrompt(action string) string {
	if action == "deny" {
		return "把这条规则写进规则文件？[u] 用户级 ~/.suga/approval-rules.yaml [p] 项目级 .suga/approval-rules.yaml [N] 只本会话："
	}
	return "把这条规则写进规则文件？[u] 用户级 ~/.suga/approval-rules.yaml [N] 只本会话："
}
