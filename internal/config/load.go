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
	privateDir, err := ResolveSuagHome()
	if err != nil {
		return fmt.Errorf("resolve suag home for config: %w", err)
	}

	// Load .env files if they exist. This allows environment variables to override
	dotenv.LoadExists(".", ".env", ".env.local")
	dotenv.LoadExistFiles(filepath.Join(privateDir, ".env"))

	gkconfig.AddDriver(yaml.Driver)
	gkconfig.WithOptions(gkconfig.ParseEnv)
	if configPath := os.Getenv("SUAG_CONFIG"); configPath != "" {
		if err := gkconfig.LoadFiles(configPath); err != nil {
			return fmt.Errorf("load SUAG_CONFIG: %w", err)
		}
		return nil
	}
	if err := gkconfig.LoadExists("./config/config.yaml", filepath.Join(privateDir, "config.yaml"), "config.yaml"); err != nil {
		slog.Error("loading config file", "err", err)
		return err
	}
	return nil
}
