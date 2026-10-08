package personal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
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
	seenPaths := make(map[string]struct{})
	previousPath := ""
	var previousContent string
	for _, path := range paths {
		raw, _, err := readRuleFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", fmt.Errorf("read rule %s: %w", path, err)
		}
		if sameLevelRulePair(previousPath, path) && previousContent == raw {
			seenPaths[rulePathKey(path)] = struct{}{}
			previousPath, previousContent = path, raw
			continue
		}
		previousPath, previousContent = path, raw
		content, err := expandRuleFile(path, &remaining, seenPaths)
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

func sameLevelRulePair(first, second string) bool {
	return strings.EqualFold(filepath.Base(first), "AGENTS.md") &&
		strings.EqualFold(filepath.Base(second), "CLAUDE.md") &&
		rulePathKey(filepath.Dir(first)) == rulePathKey(filepath.Dir(second))
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
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	return paths, nil
}

func expandRuleFile(path string, remaining *int, seenPaths map[string]struct{}) (string, error) {
	pathKey := rulePathKey(path)
	if _, exists := seenPaths[pathKey]; exists {
		return "", nil
	}
	content, truncated, err := readRuleFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read rule %s: %w", path, err)
	}
	seenPaths[pathKey] = struct{}{}
	var out strings.Builder
	// fence 记录当前所处围栏代码块的标记字符（` 或 ~），0 表示不在代码块内。
	// 代码块内的 @Test/@Override 等不是 include 指令，必须原样保留。
	var fence byte
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if marker := fenceMarker(trimmed); marker != 0 {
			if fence == 0 {
				fence = marker
			} else if fence == marker {
				fence = 0
			}
		} else if fence == 0 {
			if includePath := ruleIncludePath(trimmed); includePath != "" {
				include := filepath.Join(filepath.Dir(path), includePath)
				if sameRulePath(path, include) {
					out.WriteString("[循环引用已跳过: " + trimmed + "]\n")
					continue
				}
				includeKey := rulePathKey(include)
				if _, exists := seenPaths[includeKey]; exists {
					continue
				}
				// 目标不存在、是目录/非普通文件或读取失败时，这行很可能只是普通
				// 文本（如 "@某人 负责"），降级为保留原行，避免阻断会话创建。
				if info, statErr := os.Stat(include); statErr == nil && info.Mode().IsRegular() {
					if part, cut, readErr := readRuleFile(include); readErr == nil {
						seenPaths[includeKey] = struct{}{}
						text, dropped := appendLimited(part, remaining)
						out.WriteString(text)
						if cut {
							out.WriteString("\n[规则已截断: 单文件超过 64KB]")
						}
						if dropped {
							out.WriteString("\n[规则已截断: 总量超过 128KB]")
							break
						}
						continue
					}
				}
			}
		}
		text, dropped := appendLimited(line, remaining)
		out.WriteString(text)
		if dropped {
			out.WriteString("\n[规则已截断: 总量超过 128KB]")
			break
		}
	}
	if truncated {
		out.WriteString("\n[规则已截断: 单文件超过 64KB]")
	}
	return out.String(), nil
}

// fenceMarker returns '`' or '~' when the line opens/closes a fenced code block.
func fenceMarker(trimmed string) byte {
	if strings.HasPrefix(trimmed, "```") {
		return '`'
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return '~'
	}
	return 0
}

// ruleIncludePath accepts a line-start @path directive and ignores trailing
// whitespace-delimited explanation text.
func ruleIncludePath(line string) string {
	if !strings.HasPrefix(line, "@") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || len(fields[0]) < 2 {
		return ""
	}
	return fields[0][1:]
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
	if len(data) > maxRuleFileBytes {
		return string(data[:utf8Boundary(data, maxRuleFileBytes)]), true, nil
	}
	return string(data), false, nil
}

// appendLimited cuts content to the remaining budget on a UTF-8 boundary and
// reports whether any content was actually discarded.
func appendLimited(content string, remaining *int) (string, bool) {
	dropped := false
	if len(content) > *remaining {
		content = content[:utf8Boundary([]byte(content), *remaining)]
		dropped = true
	}
	*remaining -= len(content)
	return content, dropped
}

// utf8Boundary moves cut back so that data[:cut] does not split a multi-byte rune.
func utf8Boundary(data []byte, cut int) int {
	for cut > 0 && cut < len(data) && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return cut
}

func sameRulePath(a, b string) bool {
	return rulePathKey(a) == rulePathKey(b)
}

func rulePathKey(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	path = filepath.Clean(path)
	if os.PathSeparator == '\\' {
		path = strings.ToLower(path)
	}
	return path
}
