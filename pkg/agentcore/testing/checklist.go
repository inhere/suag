package agentcoretesting

// ContractStatus describes the current stability posture of one public surface.
type ContractStatus string

const (
	StatusHighConfidence       ContractStatus = "High confidence in v0"
	StatusStableDirection      ContractStatus = "Stable direction, careful evolution"
	StatusMustStayConservative ContractStatus = "Must stay conservative"
	StatusShouldKeepTightening ContractStatus = "Should keep tightening"
	StatusUsefulButEvolving    ContractStatus = "Useful but still evolving"
	StatusNeededForPosture     ContractStatus = "Needed for independent package posture"
	StatusHostReadyCandidate   ContractStatus = "Kernel-stable candidate"
	StatusImportantRelease     ContractStatus = "Important release surface"
)

// EvidenceLevel describes what kind of verification currently supports a surface.
type EvidenceLevel string

const (
	EvidenceRuntime     EvidenceLevel = "Runtime"
	EvidenceRuntimeHost EvidenceLevel = "Runtime + Host"
	EvidenceDefaultPath EvidenceLevel = "Default Path"
)

// ChecklistItem summarizes one contract surface together with the recommended
// verification entry points currently available in agentcore/testing.
type ChecklistItem struct {
	Surface           string
	Status            ContractStatus
	Evidence          EvidenceLevel
	RecommendedSuites []string
	Notes             string
}

// ContractChecklist returns the current package-local contract matrix in a
// machine-readable form so docs and tooling can point at the same source of truth.
func ContractChecklist() []ChecklistItem {
	return []ChecklistItem{
		{
			Surface:  "RunInput / RunState / RunResult",
			Status:   StatusHighConfidence,
			Evidence: EvidenceRuntime,
			Notes:    "Core runtime data contracts; verify together with runtime tests and release-readiness docs.",
		},
		{
			Surface:  "RunID + ResumeMode semantics",
			Status:   StatusMustStayConservative,
			Evidence: EvidenceRuntime,
			Notes:    "Resume behavior is host-sensitive and should stay aligned with transcript authority semantics.",
		},
		{
			Surface:  "Message / ToolCall / ToolResult transcript model",
			Status:   StatusHighConfidence,
			Evidence: EvidenceRuntime,
			Notes:    "Backs replay, resume, and auditability.",
		},
		{
			Surface:           "Storage authority contract",
			Status:            StatusMustStayConservative,
			Evidence:          EvidenceRuntime,
			RecommendedSuites: []string{"RunStorageSuite", "RunStorageFixtureSuite"},
			Notes:             "Use storage suites to verify run snapshots, append-only transcript order, resume replay, and storage error propagation.",
		},
		{
			Surface:           "Provider contract",
			Status:            StatusStableDirection,
			Evidence:          EvidenceRuntime,
			RecommendedSuites: []string{"RunProviderSuite"},
			Notes:             "Covers final/tool-call result shape plus optional streaming success and error paths.",
		},
		{
			Surface:           "ToolRegistry contract",
			Status:            StatusStableDirection,
			Evidence:          EvidenceRuntime,
			RecommendedSuites: []string{"RunToolRegistrySuite"},
			Notes:             "Verifies listed schema shape plus ExecuteTool result and error propagation.",
		},
		{
			Surface:  "ResumePolicy",
			Status:   StatusHostReadyCandidate,
			Evidence: EvidenceRuntimeHost,
			Notes:    "Validated in runtime tests and first-party host wrappers; lifecycle parity is not fully identical across hosts.",
		},
		{
			Surface:  "ToolExecutionPolicy",
			Status:   StatusHostReadyCandidate,
			Evidence: EvidenceRuntimeHost,
			Notes:    "Validated in runtime tests and first-party host wrappers, including escalated tool-failure behavior.",
		},
		{
			Surface:  "ErrorPolicy",
			Status:   StatusHostReadyCandidate,
			Evidence: EvidenceRuntimeHost,
			Notes:    "Validated in runtime tests and first-party host wrappers, including recoverable-error -> yielded-result behavior.",
		},
		{
			Surface:  "First-party defaults (files/memory/context/prompt/mock)",
			Status:   StatusUsefulButEvolving,
			Evidence: EvidenceDefaultPath,
			Notes:    "Useful starter path, but consumers should depend on the underlying contract rather than implementation details.",
		},
		{
			Surface:           "Conformance helpers themselves",
			Status:            StatusNeededForPosture,
			Evidence:          EvidenceRuntime,
			RecommendedSuites: []string{"RunStorageSuite", "RunStorageFixtureSuite", "RunToolRegistrySuite", "RunProviderSuite"},
			Notes:             "These helpers are part of the independent package posture and should remain public-contract-only.",
		},
		{
			Surface:  "Package-local docs / examples",
			Status:   StatusImportantRelease,
			Evidence: EvidenceDefaultPath,
			Notes:    "Important release surface for independent package posture, even though not all details are fully frozen.",
		},
	}
}
