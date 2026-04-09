package agentcoretesting

import (
	stdtesting "testing"
)

func TestContractChecklist_HasCoreConformanceEntries(t *stdtesting.T) {
	items := ContractChecklist()
	if len(items) == 0 {
		t.Fatal("expected non-empty contract checklist")
	}

	var foundStorage, foundProvider, foundTools, foundResumePolicy bool
	for _, item := range items {
		switch item.Surface {
		case "Storage authority contract":
			foundStorage = true
			if item.Evidence != EvidenceRuntime {
				t.Fatalf("storage evidence = %q, want %q", item.Evidence, EvidenceRuntime)
			}
			if len(item.RecommendedSuites) == 0 {
				t.Fatal("expected storage checklist item to recommend suites")
			}
		case "Provider contract":
			foundProvider = true
			if item.Evidence != EvidenceRuntime {
				t.Fatalf("provider evidence = %q, want %q", item.Evidence, EvidenceRuntime)
			}
		case "ToolRegistry contract":
			foundTools = true
			if item.Evidence != EvidenceRuntime {
				t.Fatalf("tool registry evidence = %q, want %q", item.Evidence, EvidenceRuntime)
			}
		case "ResumePolicy":
			foundResumePolicy = true
			if item.Evidence != EvidenceRuntimeHost {
				t.Fatalf("resume policy evidence = %q, want %q", item.Evidence, EvidenceRuntimeHost)
			}
		}
	}

	if !foundStorage {
		t.Fatal("expected storage checklist item")
	}
	if !foundProvider {
		t.Fatal("expected provider checklist item")
	}
	if !foundTools {
		t.Fatal("expected tool registry checklist item")
	}
	if !foundResumePolicy {
		t.Fatal("expected resume policy checklist item")
	}
}
