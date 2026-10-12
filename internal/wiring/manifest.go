package wiring

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	appconfig "github.com/inhere/suag/internal/config"
	"github.com/inhere/suag/internal/personal"
	capskills "github.com/inhere/suag/pkg/agentcore/skills"
	"github.com/inhere/suag/pkg/agentcore/tools"
	"github.com/inhere/suag/pkg/agentcore/tools/mcp"
	"github.com/inhere/suag/pkg/agentcore/tools/native"
)

// buildVersion is the suag version shown by the manifest; the CLI sets it
// from the binary's build info.
var buildVersion = "dev"

// SetBuildVersion records the suag version for the manifest.
func SetBuildVersion(v string) {
	if v != "" {
		buildVersion = v
	}
}

// Manifest describes what this suag instance can do (design D15). It holds
// no secrets: no API keys, base URLs, MCP arguments or environment values.
type Manifest struct {
	Version     string              `json:"version"`
	Models      ManifestModels      `json:"models"`
	Tools       []ManifestTool      `json:"tools"`
	MCPServers  []ManifestMCPServer `json:"mcp_servers"`
	Skills      ManifestSkills      `json:"skills"`
	Memory      []ManifestMemory    `json:"memory"`
	Output      ManifestOutput      `json:"output"`
	Taint       ManifestTaint       `json:"taint"`
	WritePolicy ManifestWritePolicy `json:"write_policy"`
}

// ManifestModel is one model and the provider serving it.
type ManifestModel struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ModelID       string `json:"model_id,omitempty"` // sent to the provider when it differs from model
	ContextWindow int    `json:"context_window"`
	Thinking      string `json:"thinking,omitempty"` // effective thinking level ("" = provider default)
}

// ManifestModels is the effective model selection.
type ManifestModels struct {
	Main      ManifestModel   `json:"main"`
	Compact   *ManifestModel  `json:"compact,omitempty"`
	Fallbacks []ManifestModel `json:"fallbacks,omitempty"`
}

// ManifestTool is one registered tool with its metadata.
type ManifestTool struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	ReadOnly   bool   `json:"read_only"`
	External   bool   `json:"external"`
	Rereadable bool   `json:"rereadable"`
	AgentState bool   `json:"agent_state"`
	MCP        bool   `json:"mcp"`
	MCPServer  string `json:"mcp_server,omitempty"`
	// NeedsAllowWrite: the tool changes the workspace or runs commands, so it
	// is only exposed with --allow-write (or tools.allow_write in chat).
	NeedsAllowWrite bool `json:"needs_allow_write"`
}

// ManifestMCPServer is a configured MCP server. Its tools are only listed
// when the runtime is running (the manifest command never starts servers).
type ManifestMCPServer struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}

// ManifestSkills reports the skill roots and how many skills were found.
type ManifestSkills struct {
	Roots []string `json:"roots"`
	Count int      `json:"count"`
}

// ManifestMemory is the backend serving one memory scope.
type ManifestMemory struct {
	Scope    string `json:"scope"`
	Backend  string `json:"backend"`
	Reason   string `json:"reason"`
	Location string `json:"location"`
	// Replaceable is true for every scope: any backend can be swapped for an
	// exec backend through memory.<scope>.backend.
	Replaceable bool `json:"replaceable"`
}

// ManifestOutput reports the tool output governance limits.
type ManifestOutput struct {
	InlineBytes int    `json:"inline_bytes"`
	SpillBytes  int    `json:"spill_bytes"`
	SessionsDir string `json:"sessions_dir"`
}

// ManifestTaint reports which tool results mark a session as tainted.
type ManifestTaint struct {
	TaintTools   []string `json:"taint_tools"`
	TaintOnSpill []string `json:"taint_on_spill"`
}

// ManifestWritePolicy is memory.write / memory.write_tainted.
type ManifestWritePolicy struct {
	MemoryWrite        string `json:"memory_write"`
	MemoryWriteTainted string `json:"memory_write_tainted"`
}

// ManifestInput is everything BuildManifest summarizes.
type ManifestInput struct {
	Version  string
	Settings appconfig.ModelSettings
	// Registry is the base tool registry (MCP tools included when running).
	Registry   *tools.Registry
	MCPServers []mcp.ServerSettings
	SkillRoots []capskills.Root
	Skills     *capskills.Registry
	Memory     *MemorySetup
	Policy     memoryWritePolicy

	InlineBytes, SpillBytes int
	SessionsDir             string
	TaintTools              []string
	TaintOnSpill            []string
}

// BuildManifest summarizes in. It only reads the given structures: no
// network, no process, no gofer command.
func BuildManifest(in ManifestInput) Manifest {
	m := Manifest{
		Version: in.Version,
		Models:  ManifestModels{Main: manifestModel(in.Settings.Main)},
		Output:  ManifestOutput{InlineBytes: in.InlineBytes, SpillBytes: in.SpillBytes, SessionsDir: in.SessionsDir},
		Taint: ManifestTaint{
			TaintTools:   nonNil(in.TaintTools),
			TaintOnSpill: nonNil(in.TaintOnSpill),
		},
		WritePolicy: ManifestWritePolicy{MemoryWrite: in.Policy.Write, MemoryWriteTainted: in.Policy.Tainted},
		Tools:       []ManifestTool{},
		MCPServers:  []ManifestMCPServer{},
		Memory:      []ManifestMemory{},
		Skills:      ManifestSkills{Roots: []string{}},
	}
	if in.Settings.Compact != nil {
		c := manifestModel(*in.Settings.Compact)
		m.Models.Compact = &c
	}
	for _, f := range in.Settings.Fallbacks {
		m.Models.Fallbacks = append(m.Models.Fallbacks, manifestModel(f))
	}
	if in.Registry != nil {
		names := in.Registry.List()
		slices.Sort(names)
		for _, name := range names {
			t, err := in.Registry.Get(name)
			if err != nil {
				continue
			}
			meta := tools.MetaOf(t)
			m.Tools = append(m.Tools, ManifestTool{
				Name: name, Category: string(meta.Category), ReadOnly: meta.ReadOnly, External: meta.External,
				Rereadable: meta.Rereadable, AgentState: meta.AgentState,
				MCP: tools.IsMCP(t), MCPServer: tools.MCPServerOf(t),
				NeedsAllowWrite: !meta.ReadOnly && !meta.AgentState && !tools.IsMCP(t),
			})
		}
	}
	for _, s := range in.MCPServers {
		m.MCPServers = append(m.MCPServers, ManifestMCPServer{ID: s.ID, Command: s.Command})
	}
	for _, r := range in.SkillRoots {
		m.Skills.Roots = append(m.Skills.Roots, r.Dir)
	}
	if in.Skills != nil {
		m.Skills.Count = in.Skills.Len()
	}
	if in.Memory != nil {
		for _, c := range []MemoryBackendChoice{in.Memory.User, in.Memory.Project} {
			m.Memory = append(m.Memory, ManifestMemory{
				Scope: c.Scope.String(), Backend: c.Backend, Reason: c.Reason, Location: c.Location, Replaceable: true,
			})
		}
	}
	return m
}

func manifestModel(s appconfig.ModelSpec) ManifestModel {
	m := ManifestModel{Provider: s.Provider.ID, Model: s.Model, ContextWindow: appconfig.ContextWindowFor(s), Thinking: s.ThinkingLevel()}
	if id := s.APIModel(); id != s.Model {
		m.ModelID = id
	}
	return m
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ManifestOptions selects the model and workspace of LoadManifest.
type ManifestOptions struct {
	// Model overrides the configured model (like --model).
	Model string
	// Thinking overrides the main model's thinking level (like --thinking).
	Thinking string
}

// LoadManifest builds the manifest from the current configuration without
// opening the runtime: the provider is not built or contacted, MCP servers
// are not started and the session database is not opened. Tools are the
// built-in set with every mode enabled (see ManifestTool.NeedsAllowWrite).
func LoadManifest(opts ManifestOptions) (Manifest, error) {
	workspace, err := filepath.Abs(appconfig.ResolveWorkspacePath())
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve workspace: %w", err)
	}
	st, err := loadRuntimeSettings(workspace, opts.Model, opts.Thinking)
	if err != nil {
		return Manifest{}, err
	}
	reg := tools.NewRegistry()
	for _, t := range native.BuildTools(nativeEnv(workspace), true) {
		reg.Register(t)
	}
	reg.Register(native.NewUpdatePlanTool(nil))
	mem := newMemoryRuntime(st.memSetup, st.memPolicy, false, nil, &memoryHost{})
	reg.Register(mem.tool)
	ledger := personal.NewLedger(st.sessionsDir)
	reg.Register(native.NewSessionSearchTool(ledger))
	reg.Register(native.NewRecallTool(ledger, st.inlineBytes))
	skillReg := buildDefaultSkillRegistry(workspace)
	if skillReg.Len() > 0 {
		reg.Register(native.NewSkillTool(skillReg, st.spillBytes))
	}
	reg.Register(native.NewManifestTool(nil))

	home, _ := os.UserHomeDir()
	return BuildManifest(ManifestInput{
		Version: buildVersion, Settings: st.models, Registry: reg,
		MCPServers: appconfig.ReadMCPServerSettings(),
		SkillRoots: defaultSkillRoots(workspace, home), Skills: skillReg,
		Memory: st.memSetup, Policy: st.memPolicy,
		InlineBytes: st.inlineBytes, SpillBytes: st.spillBytes, SessionsDir: st.sessionsDir,
		TaintTools: st.taintTools, TaintOnSpill: st.taintOnSpill,
	}), nil
}

// runtimeSettings are the configuration-derived inputs shared by Open and
// LoadManifest, so both read the same keys with the same validation.
type runtimeSettings struct {
	models                   appconfig.ModelSettings
	taintTools, taintOnSpill []string
	memSetup                 *MemorySetup
	memPolicy                memoryWritePolicy
	sessionsDir              string
	inlineBytes, spillBytes  int
	fold                     appconfig.FoldSettings
}

// loadRuntimeSettings reads and validates the settings that need no I/O
// beyond configuration and path resolution. It runs no backend command.
func loadRuntimeSettings(workspace, model, thinking string) (runtimeSettings, error) {
	var s runtimeSettings
	var err error
	if s.models, err = appconfig.ReadModelSettings(model); err != nil {
		return s, err
	}
	// --thinking applies to the main model only; compact and fallback models
	// keep their configured levels.
	if s.models.Main.Thinking, err = appconfig.ParseThinking(thinking); err != nil {
		return s, fmt.Errorf("--thinking: %w", err)
	}
	if s.fold, err = appconfig.ReadFoldSettings(); err != nil {
		return s, err
	}
	// Folding rewrites earlier messages, which breaks Claude's signed
	// thinking (the signature binds the request prefix) and Claude prompt
	// caching anyway: no folding when the main model speaks Anthropic.
	if s.models.Main.Provider.Type == appconfig.ProviderTypeAnthropic {
		s.fold.Every = 0
	}
	// memory.taint_tools taint on every result (default none);
	// memory.taint_on_spill only when the output was externalised (default
	// shell_exec; an empty list turns it off).
	if s.taintTools, err = toolNameList("memory.taint_tools", nil); err != nil {
		return s, err
	}
	if s.taintOnSpill, err = toolNameList("memory.taint_on_spill", []string{"shell_exec"}); err != nil {
		return s, err
	}
	memConfig, err := loadMemoryConfig()
	if err != nil {
		return s, err
	}
	if s.memPolicy, err = loadMemoryWritePolicy(); err != nil {
		return s, err
	}
	// Backend selection only (no backend command runs here).
	if s.memSetup, err = buildMemory(workspace, memConfig); err != nil {
		return s, err
	}
	if s.sessionsDir, err = appconfig.ResolveSessionsDir(); err != nil {
		return s, fmt.Errorf("resolve sessions directory: %w", err)
	}
	s.inlineBytes, s.spillBytes = inlineOutputBytes(), rawOutputBytes()
	return s, nil
}
