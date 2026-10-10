package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/inhere/suag/pkg/agentcore/api"
)

// DeferredMode is the tools.deferred setting.
type DeferredMode string

const (
	// DeferredAuto defers MCP tools only when their schemas are large relative
	// to the context window.
	DeferredAuto DeferredMode = "auto"
	// DeferredAlways always defers MCP tools.
	DeferredAlways DeferredMode = "always"
	// DeferredNever sends every tool directly.
	DeferredNever DeferredMode = "never"
)

// ParseDeferredMode validates a tools.deferred value; empty means auto.
func ParseDeferredMode(s string) (DeferredMode, error) {
	switch m := DeferredMode(strings.ToLower(strings.TrimSpace(s))); m {
	case "":
		return DeferredAuto, nil
	case DeferredAuto, DeferredAlways, DeferredNever:
		return m, nil
	}
	return "", fmt.Errorf("tools.deferred must be auto, always or never, got %q", s)
}

const (
	// defaultVisibilityWindow is used when the context window is unknown.
	defaultVisibilityWindow = 128000
	// bytesPerToken is the rough bytes-per-token ratio of the auto threshold.
	bytesPerToken = 4
	// autoDeferPercent is the share of the window MCP schemas may take before
	// they are deferred.
	autoDeferPercent = 10
	// DefaultSearchLimit and MaxSearchLimit bound tool_search results.
	DefaultSearchLimit = 5
	MaxSearchLimit     = 10
	// maxPromptDescription caps one description line in the system prompt.
	maxPromptDescription = 160
)

// Search event kinds reported through Visibility's event callback.
const (
	SearchEventSearch   = "tool_search"
	SearchEventAutoLoad = "auto_load"
)

// LoadedStore persists the session's loaded deferred tool names. The session
// is identified by ctx, so this package stays independent of the session layer.
type LoadedStore interface {
	LoadedTools(ctx context.Context) ([]string, error)
	SaveLoadedTools(ctx context.Context, names []string) error
}

// SearchEvent describes one load action for the observe layer.
type SearchEvent struct {
	Kind        string // SearchEventSearch or SearchEventAutoLoad
	Query       string
	Hits        []string
	NewlyLoaded []string
	// CacheInvalidated is true when the loaded set changed (the tool prefix of
	// the next model request differs, so the provider cache is invalidated).
	CacheInvalidated bool
}

// VisibilityOptions configures NewVisibility.
type VisibilityOptions struct {
	Mode DeferredMode
	// ContextWindow is the model window in tokens (<=0 uses 128000).
	ContextWindow int
}

// SearchHit is one tool_search match.
type SearchHit struct {
	Name          string
	Description   string
	AlreadyLoaded bool // loaded before this search
}

// SearchResult is the outcome of Visibility.Search.
type SearchResult struct {
	Hits        []SearchHit
	NewlyLoaded []string
	// Missing lists names a select: query asked for that are not deferred tools.
	Missing []string
}

type deferredTool struct {
	schema api.ToolSchema
	server string
	// nameTokens/descTokens feed tool_search scoring.
	nameTokens map[string]bool
	descTokens map[string]bool
}

// lookupRegistry is what Visibility wraps: a registry whose tools can be
// classified by Kind.
type lookupRegistry interface {
	api.ToolRegistry
	Lookup
}

// Visibility is the registry layer deciding which tools the model sees (order:
// observe -> visibility -> approval -> base). Direct tools are always listed;
// deferred (MCP) tools are listed only once loaded. Executing an unloaded
// deferred tool loads it and returns a tool error carrying its schema, without
// calling the inner layer, so no approval is requested for it.
type Visibility struct {
	inner    lookupRegistry
	deferred map[string]*deferredTool
	names    []string // deferred names, sorted

	mu      sync.Mutex
	store   LoadedStore
	onEvent func(context.Context, SearchEvent)
	mem     map[string]bool // used when no store is bound
}

var _ api.ToolRegistry = (*Visibility)(nil)
var _ Lookup = (*Visibility)(nil)

// NewVisibility classifies the inner registry's tools once. The deferred set is
// fixed for the lifetime of the layer, which keeps the prompt listing and the
// tool prefix stable within a session.
func NewVisibility(inner lookupRegistry, opts VisibilityOptions) *Visibility {
	v := &Visibility{inner: inner, deferred: map[string]*deferredTool{}, mem: map[string]bool{}}
	schemas, err := inner.ListTools(context.Background())
	if err != nil || opts.Mode == DeferredNever {
		return v
	}
	mcp := map[string]*deferredTool{}
	total := 0
	for _, s := range schemas {
		tool, err := inner.Get(s.Name)
		if err != nil || !IsMCP(tool) {
			continue
		}
		server := MCPServerOf(tool)
		d := &deferredTool{schema: s, server: server,
			nameTokens: tokenSet(s.Name), descTokens: tokenSet(s.Description)}
		mcp[s.Name] = d
		total += len(schemaJSON(s))
	}
	window := opts.ContextWindow
	if window <= 0 {
		window = defaultVisibilityWindow
	}
	// auto: defer once MCP schemas exceed 10% of the window (4 bytes per token).
	if opts.Mode == DeferredAlways || total > window*bytesPerToken*autoDeferPercent/100 {
		v.deferred = mcp
	}
	for name := range v.deferred {
		v.names = append(v.names, name)
	}
	sort.Strings(v.names)
	return v
}

// Active reports whether any tool is deferred.
func (v *Visibility) Active() bool { return v != nil && len(v.names) > 0 }

// Bind connects the persistent loaded set and the event sink. Either may be nil.
func (v *Visibility) Bind(store LoadedStore, onEvent func(context.Context, SearchEvent)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.store, v.onEvent = store, onEvent
}

// Get passes tool lookup through to the inner layers.
func (v *Visibility) Get(name string) (api.Tool, error) { return v.inner.Get(name) }

// loadedLocked returns the loaded deferred tools. Names no longer deferred
// (a server removed since) are ignored.
func (v *Visibility) loadedLocked(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	names := make([]string, 0, len(v.mem))
	if v.store != nil {
		var err error
		if names, err = v.store.LoadedTools(ctx); err != nil {
			slog.Warn("read loaded tools", "err", err)
		}
	} else {
		for n := range v.mem {
			names = append(names, n)
		}
	}
	for _, n := range names {
		if _, ok := v.deferred[n]; ok {
			out[n] = true
		}
	}
	return out
}

// addLoadedLocked merges names into the loaded set and returns the ones that
// were new. A failed save is logged: the tool is still reported as loaded for
// this process only when no store is bound.
func (v *Visibility) addLoadedLocked(ctx context.Context, loaded map[string]bool, names []string) []string {
	var added []string
	for _, n := range names {
		if _, ok := v.deferred[n]; ok && !loaded[n] {
			loaded[n] = true
			added = append(added, n)
		}
	}
	if len(added) == 0 {
		return nil
	}
	if v.store == nil {
		for _, n := range added {
			v.mem[n] = true
		}
		return added
	}
	all := make([]string, 0, len(loaded))
	for n := range loaded {
		all = append(all, n)
	}
	sort.Strings(all)
	if err := v.store.SaveLoadedTools(ctx, all); err != nil {
		slog.Warn("save loaded tools", "err", err)
	}
	return added
}

// ListTools returns direct tools followed by the loaded deferred tools sorted by name.
func (v *Visibility) ListTools(ctx context.Context) ([]api.ToolSchema, error) {
	all, err := v.inner.ListTools(ctx)
	if err != nil || len(v.deferred) == 0 {
		return all, err
	}
	out := make([]api.ToolSchema, 0, len(all))
	for _, s := range all {
		if _, ok := v.deferred[s.Name]; !ok {
			out = append(out, s)
		}
	}
	v.mu.Lock()
	loaded := v.loadedLocked(ctx)
	v.mu.Unlock()
	for _, n := range v.names { // names is sorted
		if loaded[n] {
			out = append(out, v.deferred[n].schema)
		}
	}
	return out, nil
}

// ExecuteTool runs loaded or direct tools through the inner layers. An unloaded
// deferred tool is loaded and answered with a tool error holding its schema.
func (v *Visibility) ExecuteTool(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	d, isDeferred := v.deferred[call.Name]
	if !isDeferred {
		return v.inner.ExecuteTool(ctx, call)
	}
	v.mu.Lock()
	loaded := v.loadedLocked(ctx)
	if loaded[call.Name] {
		v.mu.Unlock()
		return v.inner.ExecuteTool(ctx, call)
	}
	added := v.addLoadedLocked(ctx, loaded, []string{call.Name})
	onEvent := v.onEvent
	v.mu.Unlock()
	if onEvent != nil {
		onEvent(ctx, SearchEvent{Kind: SearchEventAutoLoad, Query: call.Name,
			Hits: []string{call.Name}, NewlyLoaded: added, CacheInvalidated: len(added) > 0})
	}
	return nil, fmt.Errorf("tool %s is not loaded yet; its schema: %s\nIt is loaded now: call it again with matching arguments",
		call.Name, schemaJSON(d.schema))
}

func schemaJSON(s api.ToolSchema) string {
	raw, err := json.Marshal(map[string]any{"name": s.Name, "description": s.Description, "input_schema": s.InputSchema})
	if err != nil {
		return s.Name
	}
	return string(raw)
}

// Search finds deferred tools and loads the hits. A query starting with
// "select:" picks exact names (comma separated); otherwise names and
// descriptions are tokenized and scored (name hits outweigh description hits).
func (v *Visibility) Search(ctx context.Context, query string, limit int) (SearchResult, error) {
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	limit = min(limit, MaxSearchLimit)
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResult{}, errors.New("query is required")
	}
	var names, missing []string
	if rest, ok := strings.CutPrefix(query, "select:"); ok {
		for _, n := range strings.Split(rest, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if _, ok := v.deferred[n]; ok {
				names = append(names, n)
			} else {
				missing = append(missing, n)
			}
		}
	} else {
		names = v.score(query, limit)
	}

	v.mu.Lock()
	loaded := v.loadedLocked(ctx)
	res := SearchResult{Missing: missing}
	for _, n := range names {
		res.Hits = append(res.Hits, SearchHit{Name: n, Description: v.deferred[n].schema.Description, AlreadyLoaded: loaded[n]})
	}
	res.NewlyLoaded = v.addLoadedLocked(ctx, loaded, names)
	onEvent := v.onEvent
	v.mu.Unlock()
	if onEvent != nil {
		hits := make([]string, 0, len(names))
		hits = append(hits, names...)
		onEvent(ctx, SearchEvent{Kind: SearchEventSearch, Query: query, Hits: hits,
			NewlyLoaded: res.NewlyLoaded, CacheInvalidated: len(res.NewlyLoaded) > 0})
	}
	return res, nil
}

// score ranks deferred tools for a free-text query: 3 per query term equal to a
// name token, 2 per term contained in a name token, 1 per term equal to a
// description token. Ties break by name.
func (v *Visibility) score(query string, limit int) []string {
	terms := tokenize(query)
	type scored struct {
		name  string
		score int
	}
	var hits []scored
	for _, n := range v.names {
		d := v.deferred[n]
		s := 0
		for _, t := range terms {
			switch {
			case d.nameTokens[t]:
				s += 3
			case containsToken(d.nameTokens, t):
				s += 2
			}
			if d.descTokens[t] {
				s++
			}
		}
		if s > 0 {
			hits = append(hits, scored{n, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]string, 0, min(limit, len(hits)))
	for i := 0; i < len(hits) && i < limit; i++ {
		out = append(out, hits[i].name)
	}
	return out
}

func containsToken(tokens map[string]bool, term string) bool {
	for tok := range tokens {
		if strings.Contains(tok, term) {
			return true
		}
	}
	return false
}

// tokenize lowercases and splits on non letter/digit runes ("_" included), so
// "github_issue_list" yields github, issue, list. Duplicates are dropped.
func tokenize(s string) []string {
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	seen := map[string]bool{}
	out := parts[:0]
	for _, p := range parts {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tokenize(s) {
		set[t] = true
	}
	return set
}

// PromptSection lists the deferred tools for the system prompt, grouped by MCP
// server and sorted by name, so it is stable for the whole session. It is empty
// when nothing is deferred.
func (v *Visibility) PromptSection() string {
	if !v.Active() {
		return ""
	}
	byServer := map[string][]string{}
	for _, n := range v.names {
		d := v.deferred[n]
		server := d.server
		if server == "" {
			server = "mcp"
		}
		byServer[server] = append(byServer[server], "- "+n+" — "+truncateDescription(d.schema.Description, maxPromptDescription))
	}
	servers := make([]string, 0, len(byServer))
	for s := range byServer {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	var b strings.Builder
	b.WriteString("## 延迟加载的工具\n")
	b.WriteString("下列 MCP 工具的参数 schema 默认不下发。需要使用时，先调用 tool_search 加载" +
		"（query 用关键字搜索，或写 select:name1,name2 精确选择）；加载后下一步即可调用。\n")
	for _, s := range servers {
		b.WriteString("\n### " + s + "\n")
		b.WriteString(strings.Join(byServer[s], "\n"))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// truncateDescription collapses whitespace and cuts to max runes with an ellipsis.
func truncateDescription(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
