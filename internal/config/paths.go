package axconfig

import (
	"os"
	"path/filepath"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

func ResolveSessionDBPath() string {
	raw := strings.TrimSpace(gkconfig.String("database_url"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if raw == "" {
		return filepath.Join("db", "suag.db")
	}
	if strings.HasPrefix(raw, "sqlite://") {
		raw = strings.TrimPrefix(raw, "sqlite://")
		if idx := strings.Index(raw, "?"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return filepath.Join("db", "suag.db")
	}
	return filepath.Clean(filepath.FromSlash(raw))
}

// ResolvePersonalSessionDBPath keeps personal conversations outside the project
// by default. An explicit database_url retains the existing path contract.
func ResolvePersonalSessionDBPath() (string, error) {
	if strings.TrimSpace(gkconfig.String("database_url")) != "" || strings.TrimSpace(os.Getenv("DATABASE_URL")) != "" {
		return ResolveSessionDBPath(), nil
	}
	dir, err := personalDBDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "suag.db"), nil
}

// ResolveCoreStorePath returns the single-file core store location
// ($SUAG_HOME/db/sessions.db). It deliberately ignores database_url, which
// only governs the legacy suag.db.
func ResolveCoreStorePath() (string, error) {
	dir, err := personalDBDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.db"), nil
}

func ResolvePersonalAgentcoreRunRoot() (string, error) {
	if strings.TrimSpace(gkconfig.String("agentcore.run_root")) != "" {
		return ResolveHostAgentcoreRunRoot(), nil
	}
	dir, err := personalDBDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agentcore", "host"), nil
}

func personalDBDir() (string, error) {
	home, err := ResolveSuagHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "db"), nil
}

// ResolveSuagHome locates user-level instructions independently of the database override.
func ResolveSuagHome() (string, error) {
	if root := strings.TrimSpace(os.Getenv("SUAG_HOME")); root != "" {
		return filepath.Abs(root)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".suag"), nil
}

func ResolveWorkspacePath() string {
	workspace := gkconfig.String("workspace")
	if workspace == "" {
		home, err := ResolveSuagHome()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "workspace")
	}
	return workspace
}

func ResolveHostAgentcoreRunRoot() string {
	root := strings.TrimSpace(gkconfig.String("agentcore.run_root"))
	if root == "" {
		return filepath.Join("db", "agentcore", "host")
	}
	return filepath.Join(filepath.Clean(filepath.FromSlash(root)), "host")
}

func ResolveHostMemoryDraftRoot() string {
	root := strings.TrimSpace(gkconfig.String("host.memory.root"))
	if root == "" {
		return filepath.Join("db", "agentcore", "host")
	}
	return filepath.Clean(filepath.FromSlash(root))
}
