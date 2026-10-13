package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/gookit/goutil/cflag/capp"
	"github.com/inhere/suag/internal/wiring"
)

// newManifestCommand builds `suag manifest`: what this instance can do,
// without contacting the provider or starting any MCP server.
func newManifestCommand(ctx context.Context, stdout, stderr io.Writer) *capp.Cmd {
	var asJSON bool
	var model, thinking string
	cmd := capp.NewCmd("manifest", "Show capabilities: models, tools, MCP, skills, memory backends, output limits", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) > 0 {
			return fmt.Errorf("usage: suag manifest [--model m] [--thinking level] [--json]")
		}
		m, err := wiring.LoadManifest(wiring.ManifestOptions{Model: model, Thinking: thinking})
		if err != nil {
			return err
		}
		if asJSON {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(m)
		}
		return writeManifestText(stdout, m)
	})
	cmd.Aliases = []string{"info"}
	cmd.SetOutput(stderr)
	cmd.StringVar(&model, "model", "", "model to report, provider/model;;m")
	cmd.StringVar(&thinking, "thinking", "", "thinking level to report (like run --thinking)")
	cmd.BoolVar(&asJSON, "json", false, "write the manifest as JSON")
	return cmd
}

func writeManifestText(out io.Writer, m wiring.Manifest) error {
	var b strings.Builder
	fmt.Fprintf(&b, "suag %s\n\n", m.Version)

	b.WriteString("Models\n")
	main := m.Models.Main
	sends := ""
	if main.ModelID != "" {
		sends = ", sends " + main.ModelID
	}
	if main.Thinking != "" {
		sends += ", thinking " + main.Thinking
	}
	fmt.Fprintf(&b, "  main      %s/%s (context %d%s)\n", main.Provider, main.Model, main.ContextWindow, sends)
	if c := m.Models.Compact; c != nil {
		fmt.Fprintf(&b, "  compact   %s/%s\n", c.Provider, c.Model)
	}
	for _, f := range m.Models.Fallbacks {
		fmt.Fprintf(&b, "  fallback  %s/%s\n", f.Provider, f.Model)
	}

	b.WriteString("\nTools\n")
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  NAME\tCATEGORY\tFLAGS")
	for _, t := range m.Tools {
		var flags []string
		for _, f := range []struct {
			on   bool
			name string
		}{
			{t.ReadOnly, "read-only"}, {t.NeedsAllowWrite, "needs-allow-write"}, {t.External, "external"},
			{t.Rereadable, "rereadable"}, {t.AgentState, "agent-state"}, {t.MCP, "mcp:" + t.MCPServer},
		} {
			if f.on {
				flags = append(flags, f.name)
			}
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", t.Name, t.Category, strings.Join(flags, ","))
	}
	_ = tw.Flush()

	b.WriteString("\nMCP servers (not started; their tools are listed only in a running session)\n")
	if len(m.MCPServers) == 0 {
		b.WriteString("  none\n")
	}
	for _, s := range m.MCPServers {
		fmt.Fprintf(&b, "  %s: %s\n", s.ID, s.Command)
	}

	fmt.Fprintf(&b, "\nSkills\n  found %d\n", m.Skills.Count)
	for _, r := range m.Skills.Roots {
		fmt.Fprintf(&b, "  root %s\n", r)
	}

	b.WriteString("\nMemory\n")
	for _, mem := range m.Memory {
		fmt.Fprintf(&b, "  %s: %s (%s) at %s, replaceable=%t\n", mem.Scope, mem.Backend, mem.Reason, mem.Location, mem.Replaceable)
	}
	fmt.Fprintf(&b, "  write=%s write_tainted=%s\n", m.WritePolicy.MemoryWrite, m.WritePolicy.MemoryWriteTainted)

	fmt.Fprintf(&b, "\nOutput\n  inline_bytes=%d spill_bytes=%d\n  sessions_dir=%s\n", m.Output.InlineBytes, m.Output.SpillBytes, m.Output.SessionsDir)
	fmt.Fprintf(&b, "\nTaint\n  taint_tools=%s\n  taint_on_spill=%s\n", strings.Join(m.Taint.TaintTools, ","), strings.Join(m.Taint.TaintOnSpill, ","))

	_, err := io.WriteString(out, b.String())
	return err
}
