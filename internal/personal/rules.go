package personal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxRuleFileBytes  = 64 * 1024
	maxRuleTotalBytes = 128 * 1024
)

// LoadRules reads user and project instructions in authority order. Includes are
// deliberately one level deep so a rule file cannot recursively expand a tree.
func LoadRules(cwd, suagHome string, noProject bool) (string, string, error) {
	var paths []string
	if suagHome != "" {
		paths = append(paths, filepath.Join(suagHome, "AGENTS.md"))
	}
	if !noProject {
		project, err := projectRulePaths(cwd)
		if err != nil {
			return "", "", err
		}
		paths = append(paths, project...)
	}
	var sections []string
	remaining := maxRuleTotalBytes
	for _, path := range paths {
		content, err := expandRuleFile(path, &remaining)
		if err != nil {
			return "", "", err
		}
		if content != "" {
			sections = append(sections, fmt.Sprintf("# %s\n%s", path, content))
		}
		if remaining == 0 {
			break
		}
	}
	rules := strings.Join(sections, "\n\n")
	hash := sha256.Sum256([]byte(rules))
	return rules, hex.EncodeToString(hash[:]), nil
}

func projectRulePaths(cwd string) ([]string, error) {
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	var levels []string
	for current := dir; ; current = filepath.Dir(current) {
		levels = append(levels, current)
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil || filepath.Dir(current) == current {
			break
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	// Without a Git root, only the requested cwd is in scope.
	if _, err := os.Stat(filepath.Join(levels[len(levels)-1], ".git")); os.IsNotExist(err) {
		levels = levels[:1]
	}
	var paths []string
	for i := len(levels) - 1; i >= 0; i-- {
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(levels[i], name)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				paths = append(paths, path)
				break
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	return paths, nil
}

func expandRuleFile(path string, remaining *int) (string, error) {
	content, truncated, err := readRuleFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read rule %s: %w", path, err)
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") && len(trimmed) > 1 && !strings.ContainsAny(trimmed, " \t") {
			include := filepath.Join(filepath.Dir(path), trimmed[1:])
			if sameRulePath(path, include) {
				out.WriteString("[循环引用已跳过: " + trimmed + "]\n")
				continue
			}
			part, cut, readErr := readRuleFile(include)
			if readErr != nil {
				return "", fmt.Errorf("read rule include %s: %w", include, readErr)
			}
			out.WriteString(appendLimited(part, remaining))
			if cut {
				out.WriteString("\n[规则已截断: 单文件超过 64KB]")
			}
			if *remaining == 0 {
				out.WriteString("\n[规则已截断: 总量超过 128KB]")
				break
			}
			continue
		}
		out.WriteString(appendLimited(line, remaining))
		if *remaining == 0 {
			out.WriteString("\n[规则已截断: 总量超过 128KB]")
			break
		}
	}
	if truncated {
		out.WriteString("\n[规则已截断: 单文件超过 64KB]")
	}
	return out.String(), nil
}

func readRuleFile(path string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRuleFileBytes+1))
	if err != nil {
		return "", false, err
	}
	return string(data[:min(len(data), maxRuleFileBytes)]), len(data) > maxRuleFileBytes, nil
}

func appendLimited(content string, remaining *int) string {
	if len(content) > *remaining {
		content = content[:*remaining]
	}
	*remaining -= len(content)
	return content
}

func sameRulePath(a, b string) bool {
	first, errA := filepath.EvalSymlinks(a)
	second, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && first == second
}
