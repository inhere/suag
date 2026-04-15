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
		return filepath.Join("db", "axon.db")
	}
	if strings.HasPrefix(raw, "sqlite://") {
		raw = strings.TrimPrefix(raw, "sqlite://")
		if idx := strings.Index(raw, "?"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return filepath.Join("db", "axon.db")
	}
	return filepath.Clean(filepath.FromSlash(raw))
}

func ResolveWorkspacePath() string {
	workspace := gkconfig.String("workspace", "~/.axon/workspace")
	if workspace == "" {
		workspace, _ = os.Getwd()
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
