package intrunner

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inhere/axon/internal/platform/memory"
)

type formalMemoryRecall struct {
	root string
}

type formalMemorySource struct {
	Name       string
	EntryCount int
	Preview    []string
}

type formalMemoryLoad struct {
	Messages []memory.Message
	Metadata map[string]any
}

func newFormalMemoryRecall(root string) *formalMemoryRecall {
	return &formalMemoryRecall{root: strings.TrimSpace(root)}
}

func (r *formalMemoryRecall) Load() formalMemoryLoad {
	if r == nil || r.root == "" {
		return formalMemoryLoad{
			Metadata: map[string]any{
				"memory_recall": map[string]any{
					"enabled":     false,
					"entry_count": 0,
					"sources":     []map[string]any{},
				},
			},
		}
	}

	files := []struct {
		name   string
		label  string
		prefix string
	}{
		{name: "MEMORY.md", label: "MEMORY", prefix: "[Host MEMORY]"},
		{name: "USER.md", label: "USER", prefix: "[Host USER]"},
	}

	var messages []memory.Message
	sources := make([]formalMemorySource, 0, len(files))
	totalEntries := 0
	for _, item := range files {
		msg, source, ok := r.loadFile(item.name, item.label, item.prefix)
		if ok {
			messages = append(messages, msg)
			sources = append(sources, source)
			totalEntries += source.EntryCount
		}
	}

	return formalMemoryLoad{
		Messages: messages,
		Metadata: map[string]any{
			"memory_recall": memoryRecallMetadata(len(messages) > 0, totalEntries, sources),
		},
	}
}

func (r *formalMemoryRecall) loadFile(name, label, prefix string) (memory.Message, formalMemorySource, bool) {
	path := filepath.Join(r.root, "memory", name)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return memory.Message{}, formalMemorySource{}, false
		}
		slog.Warn("host formal memory recall read failed", "path", path, "err", err)
		return memory.Message{}, formalMemorySource{}, false
	}

	lines := normalizeFormalMemoryLines(string(content))
	if len(lines) == 0 {
		return memory.Message{}, formalMemorySource{}, false
	}

	return memory.Message{
			Role:      "system",
			Content:   fmt.Sprintf("%s\n%s", prefix, strings.Join(lines, "\n")),
			Timestamp: time.Now().UTC(),
		}, formalMemorySource{
			Name:       label,
			EntryCount: len(lines),
			Preview:    previewFormalMemoryLines(lines, 2),
		}, true
}

func normalizeFormalMemoryLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			continue
		}
		if strings.HasPrefix(trimmed, "Updated:") {
			continue
		}
		if strings.EqualFold(trimmed, "_No applied memory entries yet._") {
			continue
		}
		out = append(out, trimmed)
	}

	return out
}

func previewFormalMemoryLines(lines []string, limit int) []string {
	if len(lines) == 0 || limit <= 0 {
		return nil
	}
	if len(lines) < limit {
		limit = len(lines)
	}
	out := make([]string, limit)
	copy(out, lines[:limit])
	return out
}

func memoryRecallMetadata(enabled bool, totalEntries int, sources []formalMemorySource) map[string]any {
	serializedSources := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		serializedSources = append(serializedSources, map[string]any{
			"name":        source.Name,
			"entry_count": source.EntryCount,
			"preview":     append([]string(nil), source.Preview...),
		})
	}
	return map[string]any{
		"enabled":     enabled,
		"entry_count": totalEntries,
		"sources":     serializedSources,
	}
}
