package rules

import "strings"

const (
	// Shell names mirror the executable categories selected by native.resolveShell.
	// They are untyped so callers can pass native.ShellName results directly.
	ShellSh         = "sh"
	ShellBash       = "bash"
	ShellPowerShell = "powershell"
	ShellCmd        = "cmd"
)

// MatchesCommand reports whether one of the rule's command patterns applies to
// target. Command rules are restricted to the native shell_exec identity. An
// allow additionally requires the command to pass the selected shell's
// conservative ASCII whitelist; ask and deny can still match an unsafe command
// so a rule can deliberately make it more restrictive.
func (r Rule) MatchesCommand(target Target) bool {
	if len(r.Command) == 0 || target.Tool != "shell_exec" || target.MCP {
		return false
	}
	for _, pattern := range r.Command {
		if commandPatternMatches(pattern, target.Command, target.Shell, target.Windows, r.Action == Allow) {
			return true
		}
	}
	return false
}

type shellSyntax uint8

const (
	syntaxUnknown shellSyntax = iota
	syntaxSh
	syntaxPowerShell
	syntaxCmd
)

// commandPatternMatches uses token equality rather than string prefixes. The
// only wildcard is a final, standalone * or ** token, which means zero or more
// trailing tokens; a wildcard embedded in a token remains literal.
func commandPatternMatches(pattern, command, shell string, windows, requireSafe bool) bool {
	syntax := classifyShell(shell)
	if command == "" || requireSafe && syntax == syntaxUnknown {
		return false
	}
	if requireSafe && !safeCommand(command, syntax) {
		return false
	}
	patternTokens, ok := commandTokens(pattern)
	if !ok || len(patternTokens) == 0 {
		return false
	}
	commandTokens, ok := commandTokens(command)
	if !ok || len(commandTokens) == 0 {
		return false
	}
	wildcard := patternTokens[len(patternTokens)-1]
	prefix := patternTokens
	if wildcard == "*" || wildcard == "**" {
		prefix = prefix[:len(prefix)-1]
		if len(commandTokens) < len(prefix) {
			return false
		}
	} else if len(commandTokens) != len(prefix) {
		return false
	}
	for i, want := range prefix {
		if !sameCommandToken(want, commandTokens[i], windows, i == 0) {
			return false
		}
	}
	return true
}

func classifyShell(name string) shellSyntax {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.LastIndexAny(name, `/\\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case ShellSh, ShellBash:
		return syntaxSh
	case ShellCmd:
		return syntaxCmd
	case ShellPowerShell, "pwsh":
		return syntaxPowerShell
	}
	// ShellName returns a descriptive value for PowerShell rather than the
	// executable basename, so accept both of its stable display forms.
	if strings.Contains(name, "powershell") || strings.Contains(name, "(pwsh)") {
		return syntaxPowerShell
	}
	return syntaxUnknown
}

func safeCommand(command string, syntax shellSyntax) bool {
	if command == "" {
		return false
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if c > 0x7f || c == '\t' || c == '\r' || c == '\n' {
			return false
		}
		if !allowedCommandByte(c, syntax) {
			return false
		}
	}
	_, ok := commandTokens(command)
	return ok
}

func allowedCommandByte(c byte, syntax shellSyntax) bool {
	if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
		return true
	}
	if strings.ContainsRune("_./:=,+@%-", rune(c)) || c == ' ' || c == '\'' || c == '"' {
		switch syntax {
		case syntaxSh, syntaxPowerShell:
			return true
		case syntaxCmd:
			return c == ' ' || c == '"' || strings.ContainsRune("_./:=,+@-", rune(c))
		}
	}
	if syntax == syntaxPowerShell && c == '\\' || syntax == syntaxCmd && c == '\\' {
		return true
	}
	return false
}

// commandTokens removes only non-nested grouping quotes. It deliberately treats
// ASCII space as the sole separator: tabs and other shell whitespace must not be
// normalized into an allow match.
func commandTokens(command string) ([]string, bool) {
	var tokens []string
	var token strings.Builder
	var quote byte
	inToken := false
	flush := func() {
		if inToken {
			tokens = append(tokens, token.String())
			token.Reset()
			inToken = false
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\'' || c == '"':
			if quote == 0 {
				quote = c
				inToken = true
			} else if quote == c {
				quote = 0
			} else {
				return nil, false
			}
		case c == ' ' && quote == 0:
			flush()
		default:
			token.WriteByte(c)
			inToken = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return tokens, true
}

func sameCommandToken(pattern, command string, windows, executable bool) bool {
	if executable && windows {
		pattern = trimExe(pattern)
		command = trimExe(command)
	}
	if windows {
		return strings.EqualFold(pattern, command)
	}
	return pattern == command
}

func trimExe(token string) string {
	return strings.TrimSuffix(strings.ToLower(token), ".exe")
}
