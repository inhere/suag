package prompt

import (
	"context"
	"testing"

	"github.com/inhere/axon/pkg/agentcore"
)

func TestLayered_BaseOnly(t *testing.T) {
	builder := Layered(Config{Base: "You are helpful."})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	if got != "You are helpful." {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, "You are helpful.")
	}
}

func TestLayered_BaseAndSections(t *testing.T) {
	builder := Layered(Config{
		Base:     "You are helpful.",
		Sections: []string{"Follow the repo rules.", "Prefer concise answers."},
	})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	want := "You are helpful.\n\nFollow the repo rules.\n\nPrefer concise answers."
	if got != want {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, want)
	}
}

func TestLayered_SkipsEmptySections(t *testing.T) {
	builder := Layered(Config{
		Sections: []string{"", "  ", "Keep answers grounded."},
	})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	if got != "Keep answers grounded." {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, "Keep answers grounded.")
	}
}

func TestLayered_ReturnsEmptyWhenAllPartsEmpty(t *testing.T) {
	builder := Layered(Config{})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	if got != "" {
		t.Fatalf("BuildSystemPrompt() = %q, want empty string", got)
	}
}

func TestLayered_DisablesMetadataByDefault(t *testing.T) {
	builder := Layered(Config{
		Base:     "You are helpful.",
		Metadata: MetadataConfig{Enabled: false},
	})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{
		Metadata: map[string]any{"mode": "resume"},
	})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	if got != "You are helpful." {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, "You are helpful.")
	}
}

func TestLayered_RendersSortedMetadata(t *testing.T) {
	builder := Layered(Config{
		Metadata: MetadataConfig{Enabled: true},
	})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{
		Metadata: map[string]any{
			"project": "agentcore",
			"attempt": 2,
		},
	})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	want := "[Run metadata]\nattempt: 2\nproject: agentcore"
	if got != want {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, want)
	}
}

func TestLayered_BaseEmptyWithMetadataAndSections(t *testing.T) {
	builder := Layered(Config{
		Metadata: MetadataConfig{Enabled: true},
		Sections: []string{"Follow repo policies."},
	})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{
		Metadata: map[string]any{
			"mode": "resume",
		},
	})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	want := "[Run metadata]\nmode: resume\n\nFollow repo policies."
	if got != want {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, want)
	}
}

func TestLayered_SkipsEmptyMetadataBlock(t *testing.T) {
	builder := Layered(Config{
		Base:     "You are helpful.",
		Metadata: MetadataConfig{Enabled: true},
	})

	got, err := builder.BuildSystemPrompt(context.Background(), &agentcore.RunState{})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	if got != "You are helpful." {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, "You are helpful.")
	}
}

func TestStaticBuilder_RemainsUnchanged(t *testing.T) {
	got, err := StaticBuilder{Prompt: "static"}.BuildSystemPrompt(context.Background(), &agentcore.RunState{
		Metadata: map[string]any{"mode": "resume"},
	})
	if err != nil {
		t.Fatalf("BuildSystemPrompt() error = %v", err)
	}

	if got != "static" {
		t.Fatalf("BuildSystemPrompt() = %q, want %q", got, "static")
	}
}
