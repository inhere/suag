package cli

import (
	"fmt"
	"io"

	"github.com/gookit/goutil/cflag/capp"
)

// newModelsCommand reserves the offline model listing entry point. The local
// catalog and its text/JSON renderers are added by the models implementation
// task; this command already owns the public --json flag and help contract.
func newModelsCommand(stdout, stderr io.Writer) *capp.Cmd {
	var asJSON bool
	models := capp.NewCmd("models", "List configured models without contacting providers", func(*capp.Cmd) error {
		_ = stdout
		_ = asJSON
		return fmt.Errorf("suga models is not implemented")
	})
	models.SetOutput(stderr)
	models.BoolVar(&asJSON, "json", false, "write the offline model list as JSON")
	return models
}
