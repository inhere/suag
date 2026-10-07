package axconfig

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/gookit/ini/v2/dotenv"
)

// Load initializes environment and YAML-backed configuration sources for the
// current process. Call it once near process startup before reading settings.
func Load() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home for config: %w", err)
	}
	privateDir := filepath.Join(home, ".suag")

	// Load .env files if they exist. This allows environment variables to override
	dotenv.LoadExists(".", ".env", ".env.local")
	dotenv.LoadExistFiles(filepath.Join(privateDir, ".env"))

	gkconfig.AddDriver(yaml.Driver)
	gkconfig.WithOptions(gkconfig.ParseEnv)
	if err := gkconfig.LoadExists("./config/config.yaml", filepath.Join(privateDir, "config.yaml"), "config.yaml"); err != nil {
		slog.Error("loading config file", "err", err)
		return err
	}
	return nil
}
