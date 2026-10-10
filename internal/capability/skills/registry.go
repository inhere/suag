package skills

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxSkillDepth bounds how deep below a root SKILL.md files are looked for
// (root/<name>/SKILL.md is depth 2; grouped layouts go one level deeper).
const maxSkillDepth = 3

// Registry holds the skills found at startup. The first registration of a
// name wins, so roots are loaded in priority order.
type Registry struct {
	skills []Skill
	byName map[string]int
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{byName: map[string]int{}} }

// Register adds s unless a skill with the same name (case-insensitive) is
// already registered; it reports whether s was added.
func (r *Registry) Register(s Skill) bool {
	key := strings.ToLower(s.Name)
	if key == "" {
		return false
	}
	if _, dup := r.byName[key]; dup {
		return false
	}
	r.byName[key] = len(r.skills)
	r.skills = append(r.skills, s)
	return true
}

// Get returns the named skill.
func (r *Registry) Get(name string) (Skill, bool) {
	if r == nil {
		return Skill{}, false
	}
	i, ok := r.byName[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Skill{}, false
	}
	return r.skills[i], true
}

// List returns the skills sorted by name.
func (r *Registry) List() []Skill {
	if r == nil {
		return nil
	}
	out := append([]Skill(nil), r.skills...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Len is the number of registered skills.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.skills)
}

// LoadRoots scans each root (in priority order) for SKILL.md files and
// registers them; a later root never replaces an earlier skill of the same
// name. Missing roots are skipped; an invalid SKILL.md is logged and skipped.
func LoadRoots(roots []string) *Registry {
	reg := NewRegistry()
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		var found []string
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if d.IsDir() {
				if path != root && (strings.HasPrefix(d.Name(), ".") || depth >= maxSkillDepth) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(d.Name(), "SKILL.md") {
				found = append(found, path)
			}
			return nil
		})
		sort.Strings(found)
		for _, path := range found {
			s, _, err := ParseFile(path)
			if err != nil {
				slog.Warn("skip invalid skill", "path", path, "err", err)
				continue
			}
			s.Path, s.Dir = path, filepath.Dir(path)
			reg.Register(s)
		}
	}
	return reg
}
