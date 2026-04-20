package intrunner

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/inhere/axon/internal/platform/memory"
)

const (
	formalMemoryFallbackPerSource = 1
	minQueryTokenLength           = 3
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

type formalMemoryFile struct {
	Label string
	Lines []string
}

func newFormalMemoryRecall(root string) *formalMemoryRecall {
	return &formalMemoryRecall{root: strings.TrimSpace(root)}
}

func (r *formalMemoryRecall) Load(instruction string) formalMemoryLoad {
	if r == nil || r.root == "" {
		return formalMemoryLoad{
			Metadata: map[string]any{
				"memory_recall": memoryRecallMetadata(false, 0, 0, false, nil),
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

	loaded := make([]formalMemoryFile, 0, len(files))
	for _, item := range files {
		lines, ok := r.loadFile(item.name)
		if !ok {
			continue
		}
		loaded = append(loaded, formalMemoryFile{
			Label: item.label,
			Lines: lines,
		})
	}

	queryTokens := extractQueryTokens(instruction)
	matchBySource := selectFormalMemoryLines(loaded, queryTokens)
	fallbackUsed := false
	matchedEntryCount := countSelectedLines(matchBySource)
	if matchedEntryCount == 0 {
		fallbackUsed = true
		matchBySource = fallbackFormalMemoryLines(loaded, formalMemoryFallbackPerSource)
	}

	var messages []memory.Message
	sources := make([]formalMemorySource, 0, len(matchBySource))
	totalEntries := 0
	for _, item := range files {
		lines := matchBySource[item.label]
		if len(lines) == 0 {
			continue
		}
		totalEntries += len(lines)
		messages = append(messages, memory.Message{
			Role:      "system",
			Content:   fmt.Sprintf("%s\n%s", item.prefix, strings.Join(lines, "\n")),
			Timestamp: time.Now().UTC(),
		})
		sources = append(sources, formalMemorySource{
			Name:       item.label,
			EntryCount: len(lines),
			Preview:    previewFormalMemoryLines(lines, 2),
		})
	}

	return formalMemoryLoad{
		Messages: messages,
		Metadata: map[string]any{
			"memory_recall": memoryRecallMetadata(
				len(messages) > 0,
				totalEntries,
				matchedEntryCount,
				fallbackUsed,
				sources,
			),
		},
	}
}

func (r *formalMemoryRecall) loadFile(name string) ([]string, bool) {
	path := filepath.Join(r.root, "memory", name)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		slog.Warn("host formal memory recall read failed", "path", path, "err", err)
		return nil, false
	}

	lines := normalizeFormalMemoryLines(string(content))
	if len(lines) == 0 {
		return nil, false
	}

	return lines, true
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

func extractQueryTokens(instruction string) []string {
	fields := strings.FieldsFunc(strings.ToLower(instruction), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(fields) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(fields))
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if utfLen := len([]rune(field)); utfLen < minQueryTokenLength {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		tokens = append(tokens, field)
	}
	sort.Strings(tokens)
	return tokens
}

func selectFormalMemoryLines(files []formalMemoryFile, queryTokens []string) map[string][]string {
	selected := make(map[string][]string, len(files))
	if len(queryTokens) == 0 {
		return selected
	}

	for _, file := range files {
		for _, line := range file.Lines {
			normalized := strings.ToLower(line)
			for _, token := range queryTokens {
				if strings.Contains(normalized, token) {
					selected[file.Label] = append(selected[file.Label], line)
					break
				}
			}
		}
	}
	return selected
}

func fallbackFormalMemoryLines(files []formalMemoryFile, limit int) map[string][]string {
	selected := make(map[string][]string, len(files))
	for _, file := range files {
		if len(file.Lines) == 0 {
			continue
		}
		fileLimit := limit
		if len(file.Lines) < fileLimit {
			fileLimit = len(file.Lines)
		}
		selected[file.Label] = append([]string(nil), file.Lines[:fileLimit]...)
	}
	return selected
}

func countSelectedLines(selected map[string][]string) int {
	total := 0
	for _, lines := range selected {
		total += len(lines)
	}
	return total
}

func memoryRecallMetadata(
	enabled bool,
	totalEntries int,
	matchedEntryCount int,
	fallbackUsed bool,
	sources []formalMemorySource,
) map[string]any {
	serializedSources := make([]map[string]any, 0, len(sources))
	for _, source := range sources {
		serializedSources = append(serializedSources, map[string]any{
			"name":        source.Name,
			"entry_count": source.EntryCount,
			"preview":     append([]string(nil), source.Preview...),
		})
	}
	return map[string]any{
		"enabled":             enabled,
		"strategy":            "query_aware_lite",
		"entry_count":         totalEntries,
		"matched_entry_count": matchedEntryCount,
		"fallback_used":       fallbackUsed,
		"sources":             serializedSources,
		"fallback_limit":      formalMemoryFallbackPerSource,
	}
}
