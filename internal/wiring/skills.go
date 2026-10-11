package wiring

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	gkconfig "github.com/gookit/config/v2"

	capskills "github.com/inhere/suag/pkg/agentcore/skills"
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
func defaultSkillRoots(workspace, homeDir string) []capskills.Root {
	roots := make([]capskills.Root, 0, 3)
	if strings.TrimSpace(workspace) != "" {
		roots = append(roots,
			capskills.Root{Dir: filepath.Join(workspace, ".agents", "skills"), Workspace: true},
			capskills.Root{Dir: filepath.Join(workspace, "skills"), Workspace: true},
		)
	}
	if strings.TrimSpace(homeDir) != "" {
		roots = append(roots, capskills.Root{Dir: filepath.Join(homeDir, ".agents", "skills")})
	}
	return roots
}

const (
	// maxSkillDescription bounds one skill line in the skills listing.
	maxSkillDescription = 160
	// defaultSkillListMax is how many skills are listed before the listing
	// falls back to workspace skills plus search (skills.list_max).
	defaultSkillListMax = 15
	// maxPreloadBytes bounds the body of one always_load skill in the prompt.
	maxPreloadBytes = 8000
)

// skillPolicy is how the skills listing (segment context) presents skills.
type skillPolicy struct {
	// listMax: with more skills than this, only workspace skills are listed
	// and the rest are found through skill(query=...).
	listMax int
	// alwaysLoad names skills (lower case) whose body goes into the prompt,
	// in addition to those marked always_load in their frontmatter.
	alwaysLoad map[string]bool
}

// readSkillPolicy reads skills.list_max and skills.always_load.
func readSkillPolicy() skillPolicy {
	p := skillPolicy{listMax: gkconfig.Int("skills.list_max", defaultSkillListMax), alwaysLoad: map[string]bool{}}
	for _, name := range gkconfig.Strings("skills.always_load") {
		p.alwaysLoad[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return p
}

// skillsPrompt is the segment context section for skills ("" when there are
// none). Preloaded (always_load) skills appear with their body. The others
// are listed as "name — description" while they number at most listMax;
// beyond that only workspace skills are listed (up to listMax) and the model
// is told how many more exist and to search for them, so a large personal
// skill collection does not cost every turn its whole listing.
func skillsPrompt(reg *capskills.Registry, pol skillPolicy) string {
	all := reg.List()
	if len(all) == 0 {
		return ""
	}
	var preload, rest []capskills.Skill
	for _, s := range all {
		if s.AlwaysLoad || pol.alwaysLoad[strings.ToLower(s.Name)] {
			preload = append(preload, s)
		} else {
			rest = append(rest, s)
		}
	}
	for name := range pol.alwaysLoad {
		if _, ok := reg.Get(name); !ok {
			slog.Warn("skills.always_load names an unknown skill", "name", name)
		}
	}
	listed := rest
	if len(rest) > pol.listMax {
		listed = nil
		for _, s := range rest {
			if s.Workspace && len(listed) < pol.listMax {
				listed = append(listed, s)
			}
		}
	}
	hidden := len(rest) - len(listed)

	var b strings.Builder
	b.WriteString("# Skills\n")
	b.WriteString("Skills are task instructions. When a task matches one, load it with the skill tool (name) before acting.\n")
	for _, s := range listed {
		desc := s.Description
		if len(desc) > maxSkillDescription {
			desc = strings.TrimSpace(desc[:utf8Cut(desc, maxSkillDescription)]) + "…"
		}
		fmt.Fprintf(&b, "- %s — %s\n", s.Name, desc)
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "%d more skills are installed but not listed. When a task may match one (a workflow, tool, "+
			"framework or kind of document), first search with the skill tool's query argument (query \"*\" lists all names) and load a match.\n", hidden)
	}
	for _, s := range preload {
		_, body, err := capskills.ParseFile(s.Path)
		if err != nil {
			slog.Warn("skip always_load skill", "name", s.Name, "err", err)
			continue
		}
		if len(body) > maxPreloadBytes {
			body = body[:utf8Cut(body, maxPreloadBytes)] + "\n…[truncated; load the rest with the skill tool]"
		}
		fmt.Fprintf(&b, "\n## Skill %s (preloaded; directory %s)\n%s\n", s.Name, s.Dir, strings.TrimSpace(body))
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
