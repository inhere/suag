// Package agentcoretesting provides package-local conformance helpers for
// validating public agentcore implementations against the expected contracts.
//
// The helpers in this package are intended for external implementers of
// agentcore.Provider, agentcore.Storage, and agentcore.ToolRegistry. They are
// deliberately small and only rely on the public agentcore contracts so they
// can be reused outside this repository.
//
// Beyond runnable suites such as RunStorageSuite, RunToolRegistrySuite, and
// RunProviderSuite, the package also exposes lightweight navigation metadata:
//
//   - ContractChecklist reports the current contract/evidence matrix
//   - OnboardingChecklist reports recommended docs, suites, and examples for
//     common external implementer roles
package agentcoretesting
