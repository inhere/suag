package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suga/internal/config"
)

// newModelsCommand renders the local model projection. It deliberately calls
// only configuration readers: no runtime, provider, MCP, hook, or network code
// is involved in this command.
func newModelsCommand(stdout, stderr io.Writer) *capp.Cmd {
	var asJSON bool
	models := capp.NewCmd("models", "List configured models without contacting providers", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("usage: suga models [--json]")
		}
		providers, err := appconfig.ReadProviders()
		if err != nil {
			return err
		}
		settings, err := appconfig.ReadModelSettings("")
		if err != nil {
			return err
		}
		catalog := appconfig.BuildModelCatalog(providers, settings)
		catalog.Source = appconfig.ModelCatalogSource()
		if asJSON {
			encoder := json.NewEncoder(stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(catalog)
		}
		return writeModelsText(stdout, catalog)
	})
	models.SetOutput(stderr)
	models.BoolVar(&asJSON, "json", false, "write the offline model list as JSON")
	return models
}

func writeModelsText(out io.Writer, catalog appconfig.ModelCatalog) error {
	var b strings.Builder
	b.WriteString("Providers (offline)\n")
	for _, provider := range catalog.Providers {
		if provider.Kind == "preset" && len(provider.ModelIDs) == 0 {
			fmt.Fprintf(&b, "  %s       preset; model ids are user-specified\n", provider.ID)
			continue
		}
		if provider.Kind == "custom" {
			fmt.Fprintf(&b, "  %s       custom; %d configured models", provider.ID, len(provider.ModelIDs))
		} else {
			fmt.Fprintf(&b, "  %s       preset; %d configured models", provider.ID, len(provider.ModelIDs))
		}
		if len(provider.ModelIDs) > 0 {
			fmt.Fprintf(&b, "; model_ids=%s", strings.Join(provider.ModelIDs, ", "))
		}
		b.WriteByte('\n')
	}

	b.WriteString("\nModels\n")
	if len(catalog.Models) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, model := range catalog.Models {
			marker := "  "
			if containsModelRole(model.Roles, "current") {
				marker = "* "
			}
			role := "configured"
			if len(model.Roles) > 0 {
				role = strings.Join(model.Roles, ",")
			}
			fmt.Fprintf(&b, "  %s%s  %-12s", marker, model.Ref, role)
			if model.ModelID != "" && model.ModelID != model.Model {
				fmt.Fprintf(&b, " model_id=%s", model.ModelID)
			}
			if model.Type != "" {
				fmt.Fprintf(&b, " type=%s", model.Type)
			}
			fmt.Fprintf(&b, " context=%d switch=%s\n", model.ContextWindow, yesNo(model.CanSwitch))
		}
	}
	b.WriteString("\n:model switch range: ")
	if len(catalog.Switchable) == 0 {
		b.WriteString("none\n")
	} else {
		b.WriteString(strings.Join(catalog.Switchable, ", "))
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "source: %s\nNo network request was made.\n", catalog.Source)
	_, err := io.WriteString(out, b.String())
	return err
}

func containsModelRole(roles []string, want string) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}
	return false
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
