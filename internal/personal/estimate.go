package personal

import (
	"context"
	"math"
	"strings"

	"github.com/inhere/suga/pkg/agentcore"
	agentcoreapi "github.com/inhere/suga/pkg/agentcore/api"
)

const (
	EstimateAuto = "auto"
	EstimateOn   = "on"
	EstimateOff  = "off"
)

// EstimateOptions controls the provider-side usage estimator. Factor is applied
// after the shared rune/token approximation and is rounded up to whole tokens.
type EstimateOptions struct {
	Mode   string
	Factor float64
}

type estimatingProvider struct {
	inner  agentcore.Provider
	mode   string
	factor float64
}

// NewEstimatingProvider wraps one concrete provider without changing its
// optional streaming, model-name, or tool-choice capabilities.
func NewEstimatingProvider(inner agentcore.Provider, options EstimateOptions) agentcore.Provider {
	if inner == nil {
		return nil
	}
	base := &estimatingProvider{inner: inner, mode: normalizeEstimateMode(options.Mode), factor: options.Factor}
	if base.factor <= 0 || math.IsNaN(base.factor) || math.IsInf(base.factor, 0) {
		base.factor = 1
	}
	stream, isStream := inner.(agentcore.StreamingProvider)
	model, hasModel := inner.(estimatingModelNamer)
	chooser, hasChoice := inner.(agentcore.ToolChooser)
	switch {
	case isStream && hasModel && hasChoice:
		return &estimatingStreamingModelToolProvider{estimatingProvider: base, stream: stream, model: model, chooser: chooser}
	case isStream && hasModel:
		return &estimatingStreamingModelProvider{estimatingProvider: base, stream: stream, model: model}
	case isStream && hasChoice:
		return &estimatingStreamingToolProvider{estimatingProvider: base, stream: stream, chooser: chooser}
	case isStream:
		return &estimatingStreamingProvider{estimatingProvider: base, stream: stream}
	case hasModel && hasChoice:
		return &estimatingModelToolProvider{estimatingProvider: base, model: model, chooser: chooser}
	case hasModel:
		return &estimatingModelProvider{estimatingProvider: base, model: model}
	case hasChoice:
		return &estimatingToolProvider{estimatingProvider: base, chooser: chooser}
	default:
		return base
	}
}

func normalizeEstimateMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case EstimateOn:
		return EstimateOn
	case EstimateOff:
		return EstimateOff
	default:
		return EstimateAuto
	}
}

func (p *estimatingProvider) Name() string { return p.inner.Name() }

func (p *estimatingProvider) Generate(ctx context.Context, req *agentcore.GenerateRequest) (*agentcore.GenerateResult, error) {
	res, err := p.inner.Generate(ctx, req)
	p.estimateResult(req, res)
	return res, err
}

func (p *estimatingProvider) estimateResult(req *agentcore.GenerateRequest, res *agentcore.GenerateResult) {
	if res == nil || p.mode == EstimateOff {
		return
	}
	input := p.estimateTokens(estimatedInputTokens(req))
	output := p.estimateTokens(estimatedOutputTokens(res))
	usage := res.Usage
	estimatedInput, estimatedOutput := false, false
	switch p.mode {
	case EstimateOn:
		// "on" records an estimate for comparison, but real provider fields
		// remain authoritative; it only fills a field that is actually absent.
		if usage.InputTokens == 0 {
			usage.InputTokens = input
			estimatedInput = true
		}
		if usage.OutputTokens == 0 {
			usage.OutputTokens = output
			estimatedOutput = true
		}
	default: // auto fills only fields the provider did not report.
		if usage.InputTokens == 0 {
			usage.InputTokens = input
			estimatedInput = true
		}
		if usage.OutputTokens == 0 {
			usage.OutputTokens = output
			estimatedOutput = true
		}
	}
	if usage.TotalTokens == 0 && (usage.InputTokens != 0 || usage.OutputTokens != 0) {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if !estimatedInput && !estimatedOutput && p.mode != EstimateOn {
		res.Usage = usage
		return
	}
	metadata := cloneResultMetadata(res.Metadata)
	if p.mode == EstimateOn {
		metadata[agentcoreapi.EstimatedInputTokensMetaKey] = input
		metadata[agentcoreapi.EstimatedOutputTokensMetaKey] = output
	}
	if estimatedInput || estimatedOutput {
		metadata[agentcoreapi.UsageEstimatedMetaKey] = true
		if p.mode != EstimateOn {
			if estimatedInput {
				metadata[agentcoreapi.EstimatedInputTokensMetaKey] = input
			}
			if estimatedOutput {
				metadata[agentcoreapi.EstimatedOutputTokensMetaKey] = output
			}
		}
	}
	res.Metadata = metadata
	res.Usage = usage
}

func (p *estimatingProvider) estimateTokens(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	scaled := math.Ceil(float64(tokens) * p.factor)
	maxInt := int(^uint(0) >> 1)
	if scaled >= float64(maxInt) {
		return maxInt
	}
	return int(scaled)
}

func estimatedInputTokens(req *agentcore.GenerateRequest) int {
	if req == nil {
		return 0
	}
	return tokensOfText(req.System) + tokensOfTools(req.Tools) + tokensOfMessages(req.Messages)
}

func estimatedOutputTokens(res *agentcore.GenerateResult) int {
	if res == nil {
		return 0
	}
	return tokensOfText(res.Content) + tokensOfText(res.Reasoning) + tokensOfToolCalls(res.ToolCalls)
}

func cloneResultMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return make(map[string]any)
	}
	cloned := make(map[string]any, len(metadata)+3)
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}

type estimatingModelNamer interface{ Model() string }

// These adapters preserve optional interfaces exactly: a plain provider does
// not accidentally become streaming or tool-choice aware.
type estimatingStreamingProvider struct {
	*estimatingProvider
	stream agentcore.StreamingProvider
}

func (p *estimatingStreamingProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	res, err := p.stream.Stream(ctx, req, emit)
	p.estimateResult(req, res)
	return res, err
}

type estimatingModelProvider struct {
	*estimatingProvider
	model estimatingModelNamer
}

func (p *estimatingModelProvider) Model() string { return p.model.Model() }

type estimatingToolProvider struct {
	*estimatingProvider
	chooser agentcore.ToolChooser
}

func (p *estimatingToolProvider) SupportsToolChoice() bool { return p.chooser.SupportsToolChoice() }

type estimatingStreamingModelProvider struct {
	*estimatingProvider
	stream agentcore.StreamingProvider
	model  estimatingModelNamer
}

func (p *estimatingStreamingModelProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	res, err := p.stream.Stream(ctx, req, emit)
	p.estimateResult(req, res)
	return res, err
}
func (p *estimatingStreamingModelProvider) Model() string { return p.model.Model() }

type estimatingStreamingToolProvider struct {
	*estimatingProvider
	stream  agentcore.StreamingProvider
	chooser agentcore.ToolChooser
}

func (p *estimatingStreamingToolProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	res, err := p.stream.Stream(ctx, req, emit)
	p.estimateResult(req, res)
	return res, err
}
func (p *estimatingStreamingToolProvider) SupportsToolChoice() bool {
	return p.chooser.SupportsToolChoice()
}

type estimatingModelToolProvider struct {
	*estimatingProvider
	model   estimatingModelNamer
	chooser agentcore.ToolChooser
}

func (p *estimatingModelToolProvider) Model() string { return p.model.Model() }
func (p *estimatingModelToolProvider) SupportsToolChoice() bool {
	return p.chooser.SupportsToolChoice()
}

type estimatingStreamingModelToolProvider struct {
	*estimatingProvider
	stream  agentcore.StreamingProvider
	model   estimatingModelNamer
	chooser agentcore.ToolChooser
}

func (p *estimatingStreamingModelToolProvider) Stream(ctx context.Context, req *agentcore.GenerateRequest, emit agentcore.StreamFunc) (*agentcore.GenerateResult, error) {
	res, err := p.stream.Stream(ctx, req, emit)
	p.estimateResult(req, res)
	return res, err
}
func (p *estimatingStreamingModelToolProvider) Model() string { return p.model.Model() }
func (p *estimatingStreamingModelToolProvider) SupportsToolChoice() bool {
	return p.chooser.SupportsToolChoice()
}
