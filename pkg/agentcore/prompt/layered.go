package prompt

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/inhere/axon/pkg/agentcore"
)

// MetadataConfig controls whether run metadata is injected into the prompt.
type MetadataConfig struct {
	Enabled bool
}

// Config defines the first-party layered prompt builder inputs.
type Config struct {
	Base     string
	Metadata MetadataConfig
	Sections []string
}

// Layered builds a system prompt from base text, optional metadata, and extra sections.
func Layered(cfg Config) agentcore.PromptBuilder {
	sections := append([]string(nil), cfg.Sections...)
	cfg.Sections = sections

	return layeredBuilder{config: cfg}
}

type layeredBuilder struct {
	config Config
}

// BuildSystemPrompt satisfies the agentcore prompt builder contract.
func (b layeredBuilder) BuildSystemPrompt(_ context.Context, state *agentcore.RunState) (string, error) {
	parts := make([]string, 0, 2+len(b.config.Sections))

	if strings.TrimSpace(b.config.Base) != "" {
		parts = append(parts, b.config.Base)
	}

	if metadataBlock := b.buildMetadataBlock(state); metadataBlock != "" {
		parts = append(parts, metadataBlock)
	}

	for _, section := range b.config.Sections {
		if strings.TrimSpace(section) == "" {
			continue
		}

		parts = append(parts, section)
	}

	return strings.Join(parts, "\n\n"), nil
}

func (b layeredBuilder) buildMetadataBlock(state *agentcore.RunState) string {
	if !b.config.Metadata.Enabled || state == nil || len(state.Metadata) == 0 {
		return ""
	}

	keys := make([]string, 0, len(state.Metadata))
	for key := range state.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys)+1)
	lines = append(lines, "[Run metadata]")
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s: %s", key, fmt.Sprint(state.Metadata[key])))
	}

	return strings.Join(lines, "\n")
}
