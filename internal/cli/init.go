package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suga/internal/config"
)

// newInitCommand builds the explicit bootstrap entry point. The startup gate
// handles all other commands before configuration loading; init owns its own
// call so it also works when no config exists yet.
func newInitCommand(stdout, stderr io.Writer) *capp.Cmd {
	var force bool
	cmd := capp.NewCmd("init", "Initialize the private suga home and onboarding templates", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("usage: suga init [--force]")
		}
		result, err := appconfig.EnsureHome(appconfig.BootstrapOptions{Force: force})
		if err != nil {
			return err
		}
		if err := writeInitResult(stdout, result); err != nil {
			return err
		}
		return appconfig.WriteBootstrapNotice(stderr, result)
	})
	cmd.SetOutput(stderr)
	cmd.BoolVar(&force, "force", false, "backup and replace the home config template")
	return cmd
}

func writeInitResult(out io.Writer, result appconfig.BootstrapResult) error {
	if result.Source == "explicit" {
		_, err := fmt.Fprintf(out, "已指定 SUGA_CONFIG=%s，未写入 home config\n", os.Getenv("SUGA_CONFIG"))
		return err
	}
	if result.Backup != "" {
		if _, err := fmt.Fprintf(out, "已备份 %s -> %s\n", result.Config, result.Backup); err != nil {
			return err
		}
	}
	if result.CreatedConfig {
		_, err := fmt.Fprintf(out, "已写入 %s\n", result.Config)
		return err
	}
	_, err := fmt.Fprintf(out, "已存在 %s，未覆盖\n", result.Config)
	return err
}
