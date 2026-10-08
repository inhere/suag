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

	// Only SUAG_HOME/.env is read. The working directory's .env belongs to the
	// user's project (suag runs inside it) and must not rewrite suag's database
	// or config root. SUAG_HOME/SUAG_CONFIG are honoured from the process
	// environment only: the root was resolved above, so a value injected by the
	// .env file is reverted to keep the config root and data root consistent.
	guarded := snapshotEnv("SUAG_HOME", "SUAG_CONFIG")
	dotenv.LoadExistFiles(filepath.Join(privateDir, ".env"))
	guarded.restore()

	gkconfig.AddDriver(yaml.Driver)
	gkconfig.WithOptions(gkconfig.ParseEnv)
	if configPath := os.Getenv("SUAG_CONFIG"); configPath != "" {
		if err := gkconfig.LoadFiles(configPath); err != nil {
			return fmt.Errorf("load SUAG_CONFIG: %w", err)
		}
		return nil
	}
	// Like .env, config.yaml is read from SUAG_HOME only: a config.yaml in the
	// user's project directory is that project's file, not suag's.
	if err := gkconfig.LoadExists(filepath.Join(privateDir, "config.yaml")); err != nil {
		slog.Error("loading config file", "err", err)
		return err
	}
	return nil
}

type envSnapshot map[string]*string

// snapshotEnv records the current process value (or absence) of each key.
func snapshotEnv(keys ...string) envSnapshot {
	snap := make(envSnapshot, len(keys))
	for _, key := range keys {
		if v, ok := os.LookupEnv(key); ok {
			snap[key] = &v
		} else {
			snap[key] = nil
		}
	}
	return snap
}

// restore puts every recorded key back to its snapshot state.
func (s envSnapshot) restore() {
	for key, v := range s {
		if v == nil {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, *v)
		}
	}
}
