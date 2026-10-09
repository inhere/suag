package app

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	capskills "github.com/inhere/suag/internal/capability/skills"
)

func SetupLogging() {
	level := slog.LevelInfo
	debugMode := os.Getenv("SUAG_DEBUG") != ""
	if debugMode {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if debugMode {
		slog.Debug("DEBUG MODE ENABLED - Verbose logging is active")
	}
}

func buildDefaultSkillRegistry(workspace string) *capskills.Registry {
	homeDir, _ := os.UserHomeDir()
	return buildSkillRegistry(workspace, homeDir)
}

func buildSkillRegistry(workspace, homeDir string) *capskills.Registry {
	registry := capskills.NewRegistry()
	for _, root := range defaultSkillRoots(workspace, homeDir) {
		loadSkillDescriptorsIntoRegistry(registry, root)
	}
	return registry
}

func defaultSkillRoots(workspace, homeDir string) []string {
	roots := make([]string, 0, 4)
	if strings.TrimSpace(homeDir) != "" {
		roots = append(roots, filepath.Join(homeDir, ".agents", "skills"))
	}
	if strings.TrimSpace(workspace) != "" {
		roots = append(roots,
			filepath.Join(workspace, ".agents", "skills"),
			filepath.Join(workspace, "skills"),
		)
	}
	roots = append(roots, filepath.Join("config", "skills"))
	return roots
}

func loadSkillDescriptorsIntoRegistry(registry *capskills.Registry, root string) {
	if registry == nil {
		return
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil || d.IsDir() {
			return nil
		}
		if !strings.EqualFold(d.Name(), "SKILL.md") {
			return nil
		}
		descriptors, err := capskills.LoadDescriptorsFromFile(path)
		if err != nil {
			return nil
		}
		for _, desc := range descriptors {
			registry.Register(desc)
		}
		return nil
	})
}
