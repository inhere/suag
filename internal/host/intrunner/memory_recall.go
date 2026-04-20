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

func newFormalMemoryRecall(root string) *formalMemoryRecall {
	return &formalMemoryRecall{root: strings.TrimSpace(root)}
}

func (r *formalMemoryRecall) Load() []memory.Message {
	if r == nil || r.root == "" {
		return nil
	}

	files := []struct {
		name   string
		prefix string
	}{
		{name: "MEMORY.md", prefix: "[Host MEMORY]"},
		{name: "USER.md", prefix: "[Host USER]"},
	}

	var messages []memory.Message
	for _, item := range files {
		msg, ok := r.loadFile(item.name, item.prefix)
		if ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

func (r *formalMemoryRecall) loadFile(name, prefix string) (memory.Message, bool) {
	path := filepath.Join(r.root, "memory", name)
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return memory.Message{}, false
		}
		slog.Warn("host formal memory recall read failed", "path", path, "err", err)
		return memory.Message{}, false
	}

	body := normalizeFormalMemory(string(content))
	if body == "" {
		return memory.Message{}, false
	}

	return memory.Message{
		Role:      "system",
		Content:   fmt.Sprintf("%s\n%s", prefix, body),
		Timestamp: time.Now().UTC(),
	}, true
}

func normalizeFormalMemory(content string) string {
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

	return strings.TrimSpace(strings.Join(out, "\n"))
}
