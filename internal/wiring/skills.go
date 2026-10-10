package wiring

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	capskills "github.com/inhere/suag/internal/capability/skills"
)

func SetupLogging() {
	level := slog.LevelInfo
	debugMode := os.Getenv("SUAG_DEBUG") != ""
	if debugMode {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if debugMode {
		slog.Debug("DEBUG MODE ENABLED - Verbose logging is active")
	}
}

// buildDefaultSkillRegistry scans the skill roots once at startup.
func buildDefaultSkillRegistry(workspace string) *capskills.Registry {
	homeDir, _ := os.UserHomeDir()
	return capskills.LoadRoots(defaultSkillRoots(workspace, homeDir))
}

// defaultSkillRoots lists the skill directories in priority order: the
// workspace's own skills first, so a project can override a user skill of the
// same name.
func defaultSkillRoots(workspace, homeDir string) []string {
	roots := make([]string, 0, 3)
	if strings.TrimSpace(workspace) != "" {
		roots = append(roots,
			filepath.Join(workspace, ".agents", "skills"),
			filepath.Join(workspace, "skills"),
		)
	}
	if strings.TrimSpace(homeDir) != "" {
		roots = append(roots, filepath.Join(homeDir, ".agents", "skills"))
	}
	return roots
}

// maxSkillDescription bounds one skill line in the system prompt.
const maxSkillDescription = 160

// skillsPrompt is the system prompt listing of the available skills ("" when
// there are none).
func skillsPrompt(reg *capskills.Registry) string {
	list := reg.List()
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Skills\n")
	b.WriteString("Skills are task instructions. When a task matches one, load it with the skill tool before acting.\n")
	for _, s := range list {
		desc := s.Description
		if len(desc) > maxSkillDescription {
			desc = strings.TrimSpace(desc[:utf8Cut(desc, maxSkillDescription)]) + "…"
		}
		fmt.Fprintf(&b, "- %s — %s\n", s.Name, desc)
	}
	return b.String()
}

// utf8Cut moves cut back to a rune start.
func utf8Cut(s string, cut int) int {
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return cut
}
