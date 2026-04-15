package axconfig

import (
	"log/slog"

	gkconfig "github.com/gookit/config/v2"
	"github.com/gookit/config/v2/yaml"
	"github.com/gookit/ini/v2/dotenv"
)

// Load initializes environment and YAML-backed configuration sources for the
// current process. Call it once near process startup before reading settings.
func Load() error {
	dotenv.LoadExists(".", ".env", ".env.local", "~/.axon/.env")

	gkconfig.AddDriver(yaml.Driver)
	gkconfig.WithOptions(gkconfig.ParseEnv)
	if err := gkconfig.LoadExists("./config/config.yaml", "~/.axon/config.yaml", "config.yaml"); err != nil {
		slog.Error("loading config file", "err", err)
		return err
	}
	return nil
}
