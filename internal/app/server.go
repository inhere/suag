package app

import (
	"context"
	"log/slog"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/axon/internal/capabilities/runtimecaps"
	capskills "github.com/inhere/axon/internal/capabilities/skills"
	appconfig "github.com/inhere/axon/internal/config"
	"github.com/inhere/axon/internal/host/acpbridge"
	"github.com/inhere/axon/internal/host/gateway"
	"github.com/inhere/axon/internal/host/intrunner"
	"github.com/inhere/axon/internal/host/session"
	"github.com/inhere/axon/internal/platform/tools"
)

// RunServer starts the axon server, which listens for incoming API requests and manages agent sessions.
func (a *App) RunServer(ctx context.Context) error {
	sessionStore, err := session.NewSQLiteStore(appconfig.ResolveSessionDBPath())
	if err != nil {
		slog.Error("failed to initialize session store", "err", err)
		return err
	}
	defer func() {
		if err := sessionStore.Close(); err != nil {
			slog.Warn("failed to close session store", "err", err)
		}
	}()

	sessionRunner, err := a.buildSessionRunner()
	if err != nil {
		slog.Error("failed to build session runner", "err", err)
		return err
	}

	sessionSvc := session.NewService(sessionStore, a.bus, sessionRunner, session.ServiceConfig{
		SessionMetadataBuilder: a.newSessionMetadataBuilder(),
		RuntimeContextBuilder:  a.newRuntimeContextBuilder(),
		RecapHooks: []session.RecapHook{
			session.NewStructuredRecapHook(sessionStore, nil),
			session.NewMemoryCandidateHook(sessionStore, nil),
			session.NewMemoryDraftHook(sessionStore, appconfig.ResolveHostMemoryDraftRoot(), nil),
		},
		MemoryFileSync: session.NewMemoryFileWriter(sessionStore, appconfig.ResolveHostMemoryDraftRoot(), nil),
	})
	h := gateway.NewHandler(a.orch, sessionStore, sessionSvc, a.bus, a.hitlMgr)
	addr := gkconfig.String("server.addr", ":8180")
	if addr == "" {
		addr = ":8180"
	}
	srv := gateway.NewServer(addr, h, a.bus)

	go a.scheduler.Start(ctx)

	if err := srv.Start(ctx); err != nil {
		slog.Error("server stopped", "err", err)
		return err
	}
	return nil
}

func (a *App) buildSessionRunner() (session.Runner, error) {
	mode := strings.TrimSpace(gkconfig.String("host.acp.mode"))
	if mode == "" {
		registry := a.registry
		if registry == nil {
			registry = tools.NewRegistry()
		}
		resolvedToolNames := a.defaultResolvedToolNames()
		return intrunner.New(intrunner.Config{
			Provider:      a.provider,
			Registry:      registry.Subset(resolvedToolNames),
			Tracer:        a.tracer,
			HITL:          a.hitlMgr,
			RunRoot:       appconfig.ResolveHostAgentcoreRunRoot(),
			MemoryRoot:    appconfig.ResolveHostMemoryDraftRoot(),
			Runtime:       gkconfig.String("agent.runtime"),
			FailThreshold: gkconfig.Int("hitl.fail_threshold", 3),
			MaxSteps:      gkconfig.Int("max_steps", 10),
		})
	}

	adapterName := strings.TrimSpace(gkconfig.String("host.acp.adapter"))
	if adapterName == "" {
		adapterName = "codex"
	}

	localCommand := strings.Fields(strings.TrimSpace(gkconfig.String("host.acp.local_command")))
	acpCommand := strings.Fields(strings.TrimSpace(gkconfig.String("host.acp.acp_command")))
	if mode == "codex_app_server" && len(acpCommand) == 0 {
		slog.Info("host acp acp_command not configured, using codex app-server default")
	}

	return acpbridge.NewRunner(acpbridge.FactoryConfig{
		Mode:                mode,
		AdapterName:         adapterName,
		Workspace:           appconfig.ResolveWorkspacePath(),
		StubRemoteAgentName: strings.TrimSpace(gkconfig.String("host.acp.stub_remote_agent_name")),
		LocalCommand: acpbridge.CommandConfig{
			Command: localCommand,
		},
		ACPCommand: acpbridge.CommandConfig{
			Command: acpCommand,
		},
	})
}

func (a *App) defaultResolvedToolNames() []string {
	if a == nil || a.capabilityManager == nil {
		if a == nil || a.registry == nil {
			return nil
		}
		return a.registry.List()
	}

	resolved := a.capabilityManager.ResolveTurnCapabilities(a.defaultCapabilitySet)
	return append([]string(nil), resolved.ToolNames...)
}

func (a *App) newRuntimeContextBuilder() session.RuntimeContextBuilder {
	if a == nil || a.capabilityManager == nil {
		return nil
	}

	return func(input session.RuntimeContextBuildInput) map[string]any {
		recallMetadata := intrunner.LoadFormalMemoryRecallMetadata(appconfig.ResolveHostMemoryDraftRoot(), input.Message)
		resolvedSkills := a.resolveRuntimeSkills(input.Message, recallMetadata)
		resolved := a.capabilityManager.ResolveTurnCapabilitiesFromInput(runtimecaps.ResolveTurnInput{
			SessionMetadata: input.SessionMetadata,
			UserMessage:     input.Message,
			AgentID:         input.AgentID,
			IsContinue:      input.IsContinue,
			RecallMetadata:  recallMetadata,
			SkillToolHints:  resolvedSkills.ToolAllowlist,
		})

		contextMap := map[string]any{
			"resolved_tool_names": append([]string(nil), resolved.ToolNames...),
			"resolved_tool_mode":  string(resolved.ToolMode),
		}
		if len(resolvedSkills.SystemPromptParts) > 0 {
			contextMap["resolved_system_fragments"] = append([]string(nil), resolvedSkills.SystemPromptParts...)
		}
		if len(resolvedSkills.ResourceHints) > 0 {
			contextMap["resolved_resource_hints"] = append([]string(nil), resolvedSkills.ResourceHints...)
		}
		if names := resolvedSkillNames(resolvedSkills.Skills); len(names) > 0 {
			contextMap["resolved_skill_names"] = names
		}
		return contextMap
	}
}

func (a *App) newSessionMetadataBuilder() session.SessionMetadataBuilder {
	if a == nil || a.capabilityManager == nil {
		return nil
	}

	defaultMetadata := a.capabilityManager.EncodeCapabilitySetMetadata(a.defaultCapabilitySet)
	return func(input session.SessionMetadataBuildInput) map[string]any {
		_ = input
		return cloneMetadataMap(defaultMetadata)
	}
}

func cloneMetadataMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}

	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = cloneMetadataValue(value)
	}
	return out
}

func cloneMetadataValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMetadataMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMetadataMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneMetadataValue(item))
		}
		return out
	default:
		return typed
	}
}

func (a *App) resolveRuntimeSkills(message string, recallMetadata map[string]any) capskills.ResolvedSkillSet {
	if a == nil || a.skillResolver == nil {
		return capskills.ResolvedSkillSet{}
	}

	return a.skillResolver.Resolve(capskills.MatchInput{
		UserMessage:   message,
		RecallPreview: recallPreviewLines(recallMetadata),
		MaxSkills:     2,
	})
}

func recallPreviewLines(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}

	recall, ok := metadata["memory_recall"].(map[string]any)
	if !ok || len(recall) == 0 {
		return nil
	}

	lines := []string{}
	switch sources := recall["sources"].(type) {
	case []map[string]any:
		for _, source := range sources {
			lines = append(lines, decodeStringSlice(source["preview"])...)
		}
	case []any:
		for _, item := range sources {
			source, ok := item.(map[string]any)
			if !ok {
				continue
			}
			lines = append(lines, decodeStringSlice(source["preview"])...)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return lines
}

func decodeStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if !ok {
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	default:
		return nil
	}
}

func resolvedSkillNames(skills []capskills.Descriptor) []string {
	if len(skills) == 0 {
		return nil
	}

	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		if name := strings.TrimSpace(skill.Name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}
