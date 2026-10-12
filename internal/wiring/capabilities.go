package wiring

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	gkconfig "github.com/gookit/config/v2"

	axconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/pkg/agentcore"
	"github.com/inhere/suag/pkg/agentcore/tools"
	toolsmcp "github.com/inhere/suag/pkg/agentcore/tools/mcp"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

type capabilityToolProvider interface {
	RegisterTools(registry *tools.Registry) error
	Close() error
}

var mcpProviderFactory = func(ctx context.Context, settings toolsmcp.ServerSettings) (capabilityToolProvider, error) {
	settings = withMCPCallTimeout(settings)
	if settings.RawBytes == 0 {
		settings.RawBytes = rawOutputBytes()
	}
	return toolsmcp.NewProvider(ctx, settings)
}

// withMCPCallTimeout applies mcp.call_timeout_sec to a server without its own
// timeout. Every server (config, Claude MCP files, ACP session) starts through
// mcpProviderFactory, so this is the one place the setting is read.
func withMCPCallTimeout(settings toolsmcp.ServerSettings) toolsmcp.ServerSettings {
	if settings.CallTimeout == 0 {
		settings.CallTimeout = axconfig.MCPCallTimeout()
	}
	return settings
}

type defaultCapabilities struct {
	Registry *tools.Registry
	closers  []capabilityToolProvider
}

// buildDefaultCapabilities assembles native and configured MCP tools for the
// personal Agent's writable mode.
func buildDefaultCapabilities(workspace string) (defaultCapabilities, error) {
	return buildDefaultCapabilitiesWithServers(context.Background(), workspace, nil)
}

func buildDefaultCapabilitiesWithServers(ctx context.Context, workspace string, additional []toolsmcp.ServerSettings) (defaultCapabilities, error) {
	registry := tools.NewRegistry()
	for _, tool := range native.BuildTools(nativeEnv(workspace), true) {
		registry.MustRegister(tool)
	}
	built := defaultCapabilities{Registry: registry}
	servers := append(axconfig.ReadMCPServerSettings(), additional...)
	for _, settings := range servers {
		provider, err := mcpProviderFactory(ctx, settings)
		if err != nil {
			_ = built.Close()
			return defaultCapabilities{}, fmt.Errorf("build mcp provider %q: %w", settings.ID, err)
		}
		if err := provider.RegisterTools(registry); err != nil {
			_ = provider.Close()
			_ = built.Close()
			return defaultCapabilities{}, fmt.Errorf("register mcp tools for %q: %w", settings.ID, err)
		}
		built.closers = append(built.closers, provider)
	}
	return built, nil
}

func (c defaultCapabilities) Close() error {
	var firstErr error
	for _, closer := range c.closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// inlineOutputBytes is tools.max_output_bytes: the largest tool result kept
// whole in the session (file_read pages to it; the output layer externalises
// anything larger).
func inlineOutputBytes() int {
	return gkconfig.Int("tools.max_output_bytes", native.DefaultMaxOutputBytes)
}

// rawOutputBytes is tools.max_spill_bytes: the raw output cap of one tool call
// (native and MCP), i.e. the most the output layer saves to a session file.
func rawOutputBytes() int {
	return gkconfig.Int("tools.max_spill_bytes", native.DefaultRawOutputBytes)
}

// warnReadRootsOnce reports rejected tools.read_roots entries once per process
// (nativeEnv runs several times).
var warnReadRootsOnce sync.Once

// readRoots returns the directories outside the workspace the read-only file
// tools may read: the detected dependency sources (tools.dependency_roots,
// default true) followed by tools.read_roots.
func readRoots() ([]native.ReadRoot, []error) {
	var roots []native.ReadRoot
	if gkconfig.Bool("tools.dependency_roots", true) {
		roots = native.DependencyRoots()
	}
	extra, errs := native.ConfiguredReadRoots(gkconfig.Strings("tools.read_roots"))
	return append(roots, extra...), errs
}

// minimizeOutput is the tool result tidier (tools.minimize_output, default
// true): test summaries, folded repeats, no colour codes; nil when off.
func minimizeOutput() func(agentcore.ToolCall, string) (string, bool) {
	if !gkconfig.Bool("tools.minimize_output", true) {
		return nil
	}
	return native.MinimizeToolOutput
}

// trashDir is where file_remove moves removed paths: <SUAG_HOME>/trash, or
// "" (file_remove unavailable) when SUAG_HOME cannot be resolved.
func trashDir() string {
	home, err := axconfig.ResolveSuagHome()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, "trash")
}

// nativeEnv reads the tools.* settings native tools are built with.
func nativeEnv(workspace string) native.Env {
	roots, errs := readRoots()
	warnReadRootsOnce.Do(func() {
		for _, err := range errs {
			slog.Warn("skip read root", "err", err)
		}
	})
	return native.Env{
		Workspace:         workspace,
		TrashDir:          trashDir(),
		ReadRoots:         native.RootPaths(roots),
		InlineBytes:       inlineOutputBytes(),
		ReadPageBytes:     gkconfig.Int("tools.read_page_bytes", native.DefaultReadPageBytes),
		RawBytes:          rawOutputBytes(),
		SerpAPIKey:        gkconfig.String("serpapi_key"),
		Shell:             gkconfig.String("tools.shell"),
		FetchAllowPrivate: gkconfig.Bool("tools.web_fetch.allow_private"),
		FetchProxy:        gkconfig.String("tools.web_fetch.proxy"),
		ShellTimeoutMax: time.Duration(gkconfig.Int("tools.shell_timeout_max",
			int(native.DefaultShellTimeoutMax/time.Second))) * time.Second,
	}
}

func swapMCPProviderFactoryForTest(factory func(ctx context.Context, settings toolsmcp.ServerSettings) (capabilityToolProvider, error)) func() {
	prev := mcpProviderFactory
	mcpProviderFactory = factory
	return func() {
		mcpProviderFactory = prev
	}
}

// newToolVisibility wraps the (approval-gated) registry in the deferred-tool
// layer per tools.deferred. When tools end up deferred, tool_search is added to
// base so the model can load them; otherwise it is not registered at all.
func newToolVisibility(base *tools.Registry, inner agentcore.ToolRegistry, contextWindow int) (*tools.Visibility, error) {
	mode, err := tools.ParseDeferredMode(gkconfig.String("tools.deferred"))
	if err != nil {
		return nil, err
	}
	lookup, ok := inner.(interface {
		agentcore.ToolRegistry
		tools.Lookup
	})
	if !ok {
		return nil, fmt.Errorf("tool registry %T does not support lookup", inner)
	}
	vis := tools.NewVisibility(lookup, tools.VisibilityOptions{Mode: mode, ContextWindow: contextWindow})
	if vis.Active() {
		base.MustRegister(native.NewToolSearchTool(vis))
	}
	return vis, nil
}
