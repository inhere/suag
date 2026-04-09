package agentcoretesting

import (
	stdtesting "testing"
)

func TestOnboardingChecklist_CoversCoreImplementerKinds(t *stdtesting.T) {
	items := OnboardingChecklist()
	if len(items) < 3 {
		t.Fatalf("expected onboarding checklist to cover at least 3 implementer kinds, got %d", len(items))
	}

	var foundStorage, foundTools, foundProvider bool
	for _, item := range items {
		if item.Kind == "" {
			t.Fatal("expected onboarding item kind to be non-empty")
		}
		if len(item.RecommendedSuites) == 0 {
			t.Fatalf("expected onboarding item %q to recommend at least one suite", item.Kind)
		}
		if len(item.Docs) == 0 {
			t.Fatalf("expected onboarding item %q to link at least one doc", item.Kind)
		}
		if len(item.Examples) == 0 {
			t.Fatalf("expected onboarding item %q to link at least one example", item.Kind)
		}

		switch item.Kind {
		case "Storage":
			foundStorage = true
		case "ToolRegistry":
			foundTools = true
		case "Provider":
			foundProvider = true
		}
	}

	if !foundStorage {
		t.Fatal("expected storage onboarding entry")
	}
	if !foundTools {
		t.Fatal("expected tool registry onboarding entry")
	}
	if !foundProvider {
		t.Fatal("expected provider onboarding entry")
	}
}
