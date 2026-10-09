package axconfig

import (
	"os"
	"path/filepath"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// ResolveCoreStorePath returns the single-file core store location
// ($SUAG_HOME/db/sessions.db).
func ResolveCoreStorePath() (string, error) {
	dir, err := personalDBDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.db"), nil
}

func personalDBDir() (string, error) {
	home, err := ResolveSuagHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "db"), nil
}

// ResolveSuagHome locates user-level instructions from the data store location.
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
	// Unset means the directory suag was started in, as the config sample
	// documents: `suag run` in a project works on that project, and a job
	// runner's working directory applies the same way the ACP session cwd does.
	if workspace := strings.TrimSpace(gkconfig.String("workspace")); workspace != "" {
		return workspace
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
