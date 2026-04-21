package intrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/inhere/axon/internal/platform/memory"
)

const runtimeContextResolvedResourceHintsKey = "resolved_resource_hints"

const maxResolvedResourceFiles = 4
const maxResolvedResourceBytes = 8 * 1024

type runtimeResourceHint struct {
	SkillName  string
	Kind       string
	Path       string
	SourcePath string
	Priority   int
}

func resolvedRuntimeContextMessages(ctxMap map[string]any) []memory.Message {
	var messages []memory.Message
	messages = append(messages, systemFragmentMessages(resolvedSystemFragmentsFromContext(ctxMap))...)
	messages = append(messages, resourceHintMessages(resolvedResourceHintsFromContext(ctxMap))...)
	if len(messages) == 0 {
		return nil
	}
	return messages
}

func resourceHintMessages(hints []runtimeResourceHint) []memory.Message {
	if len(hints) == 0 {
		return nil
	}

	sort.SliceStable(hints, func(i, j int) bool {
		if hints[i].Priority != hints[j].Priority {
			return hints[i].Priority > hints[j].Priority
		}
		return hints[i].Path < hints[j].Path
	})

	out := make([]memory.Message, 0, minInt(len(hints), maxResolvedResourceFiles))
	for _, hint := range hints {
		if len(out) >= maxResolvedResourceFiles {
			break
		}
		msg, ok := resourceHintMessage(hint)
		if !ok {
			continue
		}
		out = append(out, msg)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resourceHintMessage(hint runtimeResourceHint) (memory.Message, bool) {
	hint = normalizeRuntimeResourceHint(hint)
	if hint.Path == "" || hint.SourcePath == "" {
		return memory.Message{}, false
	}

	if !resourceHintAllowed(hint) {
		return memory.Message{}, false
	}

	info, err := os.Stat(hint.Path)
	if err != nil || !info.Mode().IsRegular() {
		return memory.Message{}, false
	}

	content, err := os.ReadFile(hint.Path)
	if err != nil {
		return memory.Message{}, false
	}
	if len(content) > maxResolvedResourceBytes {
		content = content[:maxResolvedResourceBytes]
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return memory.Message{}, false
	}

	return memory.Message{
		Role: "system",
		Content: fmt.Sprintf(
			"[Skill Resource %s/%s %s]\n%s",
			hint.SkillName,
			hint.Kind,
			filepath.Base(hint.Path),
			trimmed,
		),
		Timestamp: time.Now().UTC(),
	}, true
}

func resolvedResourceHintsFromContext(ctxMap map[string]any) []runtimeResourceHint {
	if len(ctxMap) == 0 {
		return nil
	}

	raw, ok := ctxMap[runtimeContextResolvedResourceHintsKey]
	if !ok {
		return nil
	}

	switch value := raw.(type) {
	case []map[string]any:
		return normalizeRuntimeResourceHints(decodeMapResourceHints(value))
	case []string:
		out := make([]runtimeResourceHint, 0, len(value))
		for _, path := range normalizeStringSlice(value) {
			out = append(out, runtimeResourceHint{Path: path})
		}
		return normalizeRuntimeResourceHints(out)
	case []any:
		out := make([]runtimeResourceHint, 0, len(value))
		for _, item := range value {
			switch typed := item.(type) {
			case string:
				text := strings.TrimSpace(typed)
				if text == "" {
					continue
				}
				out = append(out, runtimeResourceHint{Path: text})
			case map[string]any:
				out = append(out, decodeMapResourceHint(typed))
			}
		}
		return normalizeRuntimeResourceHints(out)
	default:
		return nil
	}
}

func decodeMapResourceHints(values []map[string]any) []runtimeResourceHint {
	out := make([]runtimeResourceHint, 0, len(values))
	for _, value := range values {
		out = append(out, decodeMapResourceHint(value))
	}
	return out
}

func decodeMapResourceHint(value map[string]any) runtimeResourceHint {
	hint := runtimeResourceHint{
		SkillName:  decodeStringValue(value["skill"]),
		Kind:       decodeStringValue(value["kind"]),
		Path:       decodeStringValue(value["path"]),
		SourcePath: decodeStringValue(value["source_path"]),
	}
	switch typed := value["priority"].(type) {
	case int:
		hint.Priority = typed
	case int64:
		hint.Priority = int(typed)
	case float64:
		hint.Priority = int(typed)
	}
	return hint
}

func decodeStringValue(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func normalizeRuntimeResourceHints(values []runtimeResourceHint) []runtimeResourceHint {
	if len(values) == 0 {
		return nil
	}

	out := make([]runtimeResourceHint, 0, len(values))
	for _, value := range values {
		value = normalizeRuntimeResourceHint(value)
		if value.Path == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeRuntimeResourceHint(value runtimeResourceHint) runtimeResourceHint {
	value.SkillName = strings.TrimSpace(value.SkillName)
	value.Kind = strings.TrimSpace(strings.ToLower(value.Kind))
	value.Path = strings.TrimSpace(value.Path)
	value.SourcePath = strings.TrimSpace(value.SourcePath)
	return value
}

func resourceHintAllowed(hint runtimeResourceHint) bool {
	hint = normalizeRuntimeResourceHint(hint)
	if hint.Path == "" || hint.SourcePath == "" {
		return false
	}
	if !isAllowedResourceExtension(hint.Path) {
		return false
	}

	skillRoot, err := filepath.Abs(filepath.Dir(hint.SourcePath))
	if err != nil {
		return false
	}
	targetPath, err := filepath.Abs(hint.Path)
	if err != nil {
		return false
	}

	relToSkillRoot, err := filepath.Rel(skillRoot, targetPath)
	if err != nil || pathEscapesRoot(relToSkillRoot) {
		return false
	}
	relToSkillRoot = filepath.ToSlash(relToSkillRoot)
	if !strings.HasPrefix(relToSkillRoot, "references/") && !strings.HasPrefix(relToSkillRoot, "assets/") {
		return false
	}
	return true
}

func pathEscapesRoot(rel string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == ".." || strings.HasPrefix(rel, "../")
}

func isAllowedResourceExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".md", ".txt", ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
