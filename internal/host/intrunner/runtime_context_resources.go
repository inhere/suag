package intrunner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inhere/axon/internal/platform/memory"
)

const runtimeContextResolvedResourceHintsKey = "resolved_resource_hints"

const maxResolvedResourceFiles = 4
const maxResolvedResourceBytes = 8 * 1024

func resolvedRuntimeContextMessages(ctxMap map[string]any) []memory.Message {
	var messages []memory.Message
	messages = append(messages, systemFragmentMessages(resolvedSystemFragmentsFromContext(ctxMap))...)
	messages = append(messages, resourceHintMessages(resolvedResourceHintsFromContext(ctxMap))...)
	if len(messages) == 0 {
		return nil
	}
	return messages
}

func resourceHintMessages(paths []string) []memory.Message {
	if len(paths) == 0 {
		return nil
	}

	out := make([]memory.Message, 0, minInt(len(paths), maxResolvedResourceFiles))
	for _, path := range paths {
		if len(out) >= maxResolvedResourceFiles {
			break
		}
		msg, ok := resourceHintMessage(path)
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

func resourceHintMessage(path string) (memory.Message, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return memory.Message{}, false
	}

	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return memory.Message{}, false
	}

	content, err := os.ReadFile(path)
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
			"[Skill Resource %s]\n%s",
			filepath.Base(path),
			trimmed,
		),
		Timestamp: time.Now().UTC(),
	}, true
}

func resolvedResourceHintsFromContext(ctxMap map[string]any) []string {
	if len(ctxMap) == 0 {
		return nil
	}

	raw, ok := ctxMap[runtimeContextResolvedResourceHintsKey]
	if !ok {
		return nil
	}

	switch value := raw.(type) {
	case []string:
		return normalizeStringSlice(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return normalizeStringSlice(out)
	default:
		return nil
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
