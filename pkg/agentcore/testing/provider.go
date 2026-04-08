package agentcoretesting

import (
	"context"
	"errors"
	"reflect"
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
)

// GenerateCase describes one expected Generate call against a provider.
type GenerateCase struct {
	Name       string
	Request    *agentcore.GenerateRequest
	WantResult *agentcore.GenerateResult
	WantErr    error
}

// NewGenerateCase creates a named provider generate contract case.
func NewGenerateCase(name string, req *agentcore.GenerateRequest, want *agentcore.GenerateResult) GenerateCase {
	return GenerateCase{
		Name:       name,
		Request:    req,
		WantResult: want,
	}
}

// StreamingCase describes one optional streaming provider scenario.
type StreamingCase struct {
	Name       string
	Request    *agentcore.GenerateRequest
	WantEvents []agentcore.StreamEvent
	WantResult *agentcore.GenerateResult
	WantErr    error
	EmitErr    error
}

// ProviderFixture configures provider conformance checks.
type ProviderFixture struct {
	Provider       agentcore.Provider
	StreamProvider agentcore.StreamingProvider
	GenerateCases  []GenerateCase
	Streaming      *StreamingCase
	StreamingCases []StreamingCase
}

// NewProviderFixture creates a minimal provider conformance fixture.
func NewProviderFixture(provider agentcore.Provider, cases ...GenerateCase) ProviderFixture {
	return ProviderFixture{
		Provider:      provider,
		GenerateCases: append([]GenerateCase(nil), cases...),
	}
}

// WithGenerateError appends one expected provider error case.
func (f ProviderFixture) WithGenerateError(name string, req *agentcore.GenerateRequest, wantErr error) ProviderFixture {
	f.GenerateCases = append(f.GenerateCases, GenerateCase{
		Name:    name,
		Request: req,
		WantErr: wantErr,
	})
	return f
}

// WithStreamingProvider attaches a dedicated streaming-capable provider for stream cases.
func (f ProviderFixture) WithStreamingProvider(provider agentcore.StreamingProvider) ProviderFixture {
	f.StreamProvider = provider
	return f
}

// WithStreaming returns a copy of the fixture with one streaming case attached.
func (f ProviderFixture) WithStreaming(tc StreamingCase) ProviderFixture {
	f.StreamingCases = append(f.StreamingCases, tc)
	if f.Streaming == nil {
		copy := tc
		f.Streaming = &copy
	}
	return f
}

// WithStreamingError appends one expected streaming provider error case.
func (f ProviderFixture) WithStreamingError(tc StreamingCase) ProviderFixture {
	f.StreamingCases = append(f.StreamingCases, tc)
	if f.Streaming == nil {
		copy := tc
		f.Streaming = &copy
	}
	return f
}

// WithStreamingHandlerError appends one expected stream-handler failure case.
func (f ProviderFixture) WithStreamingHandlerError(tc StreamingCase) ProviderFixture {
	f.StreamingCases = append(f.StreamingCases, tc)
	if f.Streaming == nil {
		copy := tc
		f.Streaming = &copy
	}
	return f
}

// ProviderFactory returns a fresh provider fixture for one test case.
type ProviderFactory func(t *stdtesting.T) ProviderFixture

// RunProviderSuite runs the minimal public provider conformance checks.
func RunProviderSuite(t *stdtesting.T, factory ProviderFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("agentcoretesting: provider factory is nil")
	}

	t.Run("generate_cases_return_valid_results", func(t *stdtesting.T) {
		t.Helper()
		fixture := factory(t)
		if fixture.Provider == nil {
			t.Fatal("agentcoretesting: provider fixture returned nil provider")
		}
		if len(fixture.GenerateCases) == 0 {
			t.Fatal("agentcoretesting: provider fixture returned no generate cases")
		}

		for _, tc := range fixture.GenerateCases {
			name := tc.Name
			if name == "" {
				name = "generate"
			}
			t.Run(name, func(t *stdtesting.T) {
				result, err := fixture.Provider.Generate(context.Background(), tc.Request)
				if tc.WantErr != nil {
					if !errors.Is(err, tc.WantErr) {
						t.Fatalf("expected Generate error %v, got result=%#v err=%v", tc.WantErr, result, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("expected Generate to succeed, got %v", err)
				}
				if err := result.Validate(); err != nil {
					t.Fatalf("expected provider result to satisfy GenerateResult.Validate, got %v", err)
				}
				if !reflect.DeepEqual(result, tc.WantResult) {
					t.Fatalf("expected generate result %#v, got %#v", tc.WantResult, result)
				}
			})
		}
	})

	t.Run("streaming_cases_are_optional_and_match_expected_contract", func(t *stdtesting.T) {
		t.Helper()
		fixture := factory(t)
		streamingCases := fixture.streamingCases()
		if len(streamingCases) == 0 {
			t.Skip("agentcoretesting: no streaming case configured")
		}

		streamingProvider := fixture.StreamProvider
		if streamingProvider == nil {
			var ok bool
			streamingProvider, ok = fixture.Provider.(agentcore.StreamingProvider)
			if !ok {
				t.Fatalf("expected provider %T to implement StreamingProvider for streaming case", fixture.Provider)
			}
		}

		for _, tc := range streamingCases {
			name := tc.Name
			if name == "" {
				name = "stream"
			}
			t.Run(name, func(t *stdtesting.T) {
				var events []agentcore.StreamEvent
				result, err := streamingProvider.Stream(context.Background(), tc.Request, func(_ context.Context, event agentcore.StreamEvent) error {
					events = append(events, event)
					if tc.EmitErr != nil {
						return tc.EmitErr
					}
					return nil
				})
				if !reflect.DeepEqual(events, tc.WantEvents) {
					t.Fatalf("expected streaming events %#v, got %#v", tc.WantEvents, events)
				}
				if tc.WantErr != nil {
					if !errors.Is(err, tc.WantErr) {
						t.Fatalf("expected Stream error %v, got result=%#v err=%v", tc.WantErr, result, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("expected Stream to succeed, got %v", err)
				}
				if err := result.Validate(); err != nil {
					t.Fatalf("expected streaming final result to satisfy GenerateResult.Validate, got %v", err)
				}
				if !reflect.DeepEqual(result, tc.WantResult) {
					t.Fatalf("expected streaming final result %#v, got %#v", tc.WantResult, result)
				}
			})
		}
	})
}

func (f ProviderFixture) streamingCases() []StreamingCase {
	if len(f.StreamingCases) > 0 {
		return append([]StreamingCase(nil), f.StreamingCases...)
	}
	if f.Streaming == nil {
		return nil
	}
	return []StreamingCase{*f.Streaming}
}
