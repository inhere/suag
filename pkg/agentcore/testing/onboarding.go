package agentcoretesting

// OnboardingItem describes the recommended starting points for one external
// implementation category.
type OnboardingItem struct {
	Kind              string
	RecommendedSuites []string
	Docs              []string
	Examples          []string
	Notes             string
}

// OnboardingChecklist returns a small package-local navigation guide for
// external implementers who want to validate a custom agentcore integration.
func OnboardingChecklist() []OnboardingItem {
	return []OnboardingItem{
		{
			Kind:              "Storage",
			RecommendedSuites: []string{"RunStorageSuite", "RunStorageFixtureSuite"},
			Docs: []string{
				"docs/api.md",
				"docs/conformance-testing.md",
				"docs/contract-matrix.md",
			},
			Examples: []string{
				"testing/README.md",
				"testing/external_examples_test.go#TestExternalStorageContractExample",
			},
			Notes: "Start with happy-path authority checks, then add error-propagation coverage if your storage has custom failure modes.",
		},
		{
			Kind:              "ToolRegistry",
			RecommendedSuites: []string{"RunToolRegistrySuite"},
			Docs: []string{
				"docs/api.md",
				"docs/conformance-testing.md",
				"docs/contract-matrix.md",
			},
			Examples: []string{
				"testing/README.md",
				"testing/external_examples_test.go#TestExternalToolRegistryContractExample",
			},
			Notes: "Validate listed schema shape first, then add execute cases for both success and expected tool errors.",
		},
		{
			Kind:              "Provider",
			RecommendedSuites: []string{"RunProviderSuite"},
			Docs: []string{
				"docs/api.md",
				"docs/conformance-testing.md",
				"docs/contract-matrix.md",
				"docs/release-readiness.md",
			},
			Examples: []string{
				"testing/README.md",
				"testing/external_examples_test.go#TestExternalProviderContractExample",
			},
			Notes: "Cover minimal Generate contract first, then add streaming success and error-path cases only if your provider exposes streaming.",
		},
	}
}
