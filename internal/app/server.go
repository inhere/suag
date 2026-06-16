package app

import (
	"context"
	"log/slog"
	"strings"

	gkconfig "github.com/gookit/config/v2"

	"github.com/inhere/suag/internal/capability/runtimecaps"
	capskills "github.com/inhere/suag/internal/capability/skills"
	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/host/acpbridge"
	"github.com/inhere/suag/internal/host/gateway"
	"github.com/inhere/suag/internal/host/intrunner"
	"github.com/inhere/suag/internal/host/session"
	"github.com/inhere/suag/internal/platform/tools"
)

// RunServer starts the suag server, which listens for incoming API requests and manages agent sessions.
func (a *App) RunServer(ctx context.Context) error {
	sessionDBPath := appconfig.ResolveSessionDBPath()
	if err := prepareHostSessionDatabase(ctx, sessionDBPath); err != nil {
		slog.Error("failed to prepare session database", "err", err)
		return err
	}

	sessionRepo, err := session.NewSQLiteRepository(sessionDBPath)
	if err != nil {
		slog.Error("failed to initialize session repository", "err", err)
		return err
	}
	defer func() {
		if err := sessionRepo.Close(); err != nil {
			slog.Warn("failed to close session repository", "err", err)
		}
	}()

	sessionRunner, err := a.buildSessionRunner()
	if err != nil {
		slog.Error("failed to build session runner", "err", err)
		return err
	}

	sessionSvc := session.NewService(sessionRepo, a.bus, sessionRunner, session.ServiceConfig{
		SessionMetadataBuilder: a.newSessionMetadataBuilder(),
		RuntimeContextBuilder:  a.newRuntimeContextBuilder(),
		RecapHooks: []session.RecapHook{
			session.NewStructuredRecapHook(sessionRepo, nil),
			session.NewMemoryCandidateHook(sessionRepo, nil),
			session.NewMemoryProjectionHook(sessionRepo, appconfig.ResolveHostMemoryDraftRoot(), nil),
		},
		PostTurnActionHooks: []session.PostTurnActionHook{
			session.NewCapabilityPlannerHook(),
		},
		MemoryProjectionSync: session.NewMemoryMarkdownProjector(sessionRepo, appconfig.ResolveHostMemoryDraftRoot(), nil),
	})
	h := gateway.NewHandler(a.orch, sessionRepo, sessionSvc, a.bus, a.hitlMgr)
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
		resolvedSkills := a.resolveRuntimeSkills(input.Message, recallMetadata, input.PreviousNextTurnPlan)
		registeredSkillInventory := a.registeredSkillInventory(input.Message)
		resolved := a.capabilityManager.ResolveTurnCapabilitiesFromInput(runtimecaps.ResolveTurnInput{
			SessionMetadata:      input.SessionMetadata,
			UserMessage:          input.Message,
			AgentID:              input.AgentID,
			IsContinue:           input.IsContinue,
			RecallMetadata:       recallMetadata,
			SkillToolHints:       resolvedSkills.ToolAllowlist,
			PreviousNextTurnPlan: input.PreviousNextTurnPlan,
		})

		contextMap := map[string]any{
			"resolved_tool_names": append([]string(nil), resolved.ToolNames...),
			"resolved_tool_refs":  encodeRuntimeToolRefs(resolved.ToolRefs),
			"resolved_tool_mode":  string(resolved.ToolMode),
			"memory_recall_hit":   recallHit(recallMetadata),
		}
		systemFragments := append([]string(nil), resolvedSkills.SystemPromptParts...)
		if registeredSkillInventory != "" {
			systemFragments = append(systemFragments, registeredSkillInventory)
		}
		if len(systemFragments) > 0 {
			contextMap["resolved_system_fragments"] = systemFragments
		}
		if len(resolvedSkills.ResourceHints) > 0 {
			contextMap["resolved_resource_hints"] = encodeResourceHints(resolvedSkills.ResourceHints)
		}
		if names := resolvedSkillNames(resolvedSkills.Skills); len(names) > 0 {
			contextMap["resolved_skill_names"] = names
		}
		if matches := encodeSkillMatches(resolvedSkills.Matches); len(matches) > 0 {
			contextMap["resolved_skill_matches"] = matches
		}
		return contextMap
	}
}

func encodeRuntimeToolRefs(refs []runtimecaps.ToolRef) []map[string]any {
	if len(refs) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		if ref.Name == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":        ref.Name,
			"source":      ref.Source,
			"provider_id": ref.ProviderID,
		})
	}
	return out
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

func (a *App) resolveRuntimeSkills(message string, recallMetadata map[string]any, previousNextTurnPlan map[string]any) capskills.ResolvedSkillSet {
	if a == nil || a.skillResolver == nil {
		return capskills.ResolvedSkillSet{}
	}

	return a.skillResolver.Resolve(capskills.MatchInput{
		UserMessage:         message,
		RecallPreview:       recallPreviewLines(recallMetadata),
		PreferredSkillNames: previousPlanSkillNames(previousNextTurnPlan),
		MaxSkills:           2,
	})
}

func (a *App) registeredSkillInventory(message string) string {
	if a == nil || a.skillRegistry == nil || !isSkillInventoryQuestion(message) {
		return ""
	}
	descriptors := a.skillRegistry.List()
	if len(descriptors) == 0 {
		return "[Registered Skills]\nNo registered skills were discovered from configured skill roots."
	}

	lines := []string{
		"[Registered Skills]",
		"These skills are registered and available for matching. They are not necessarily injected into every turn unless selected by capability matching.",
	}
	for _, desc := range descriptors {
		name := strings.TrimSpace(desc.Name)
		if name == "" {
			continue
		}
		description := strings.TrimSpace(desc.Description)
		source := strings.TrimSpace(desc.SourcePath)
		line := "- " + name
		if description != "" {
			line += ": " + description
		}
		if source != "" {
			line += " (" + source + ")"
		}
		lines = append(lines, line)
	}
	if len(lines) <= 2 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func isSkillInventoryQuestion(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "skill") ||
		strings.Contains(normalized, "技能") ||
		strings.Contains(normalized, "能力") ||
		strings.Contains(normalized, "加载")
}

func previousPlanSkillNames(plan map[string]any) []string {
	if len(plan) == 0 {
		return nil
	}
	return decodeStringSlice(plan["skill_names"])
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

func recallHit(metadata map[string]any) bool {
	return len(recallPreviewLines(metadata)) > 0
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

func encodeResourceHints(hints []capskills.ResourceHint) []map[string]any {
	if len(hints) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(hints))
	for _, hint := range hints {
		if strings.TrimSpace(hint.Path) == "" {
			continue
		}
		out = append(out, map[string]any{
			"skill":       strings.TrimSpace(hint.SkillName),
			"kind":        strings.TrimSpace(hint.Kind),
			"path":        strings.TrimSpace(hint.Path),
			"source_path": strings.TrimSpace(hint.SourcePath),
			"priority":    hint.Priority,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeSkillMatches(matches []capskills.SkillMatch) []map[string]any {
	if len(matches) == 0 {
		return nil
	}

	out := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		if strings.TrimSpace(match.SkillName) == "" || match.Score <= 0 {
			continue
		}
		out = append(out, map[string]any{
			"skill":         strings.TrimSpace(match.SkillName),
			"score":         match.Score,
			"match_sources": append([]string(nil), match.MatchSources...),
			"match_terms":   append([]string(nil), match.MatchTerms...),
			"scope_hints":   append([]string(nil), match.ScopeHints...),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
