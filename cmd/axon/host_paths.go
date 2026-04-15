package main

import (
	"path/filepath"
	"strings"

	"github.com/gookit/config/v2"
)

func resolveHostAgentcoreRunRoot() string {
	root := strings.TrimSpace(config.String("agentcore.run_root"))
	if root == "" {
		return filepath.Join("db", "agentcore", "host")
	}
	return filepath.Join(filepath.Clean(filepath.FromSlash(root)), "host")
}
