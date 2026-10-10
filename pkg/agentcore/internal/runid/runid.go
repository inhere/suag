// Package runid validates run identifiers shared by agentcore storages.
package runid

import (
	"errors"
	"path/filepath"
	"strings"
)

// Validate reports why runID is unsafe, or nil. A valid run id is a single
// path segment: storages may use it as a directory name, so absolute paths,
// separators and "."/".." are rejected everywhere for the same behavior.
func Validate(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run id is required")
	}
	if filepath.IsAbs(runID) || filepath.VolumeName(runID) != "" {
		return errors.New("run id must not be an absolute path")
	}
	if runID == "." || runID == ".." {
		return errors.New("run id must not reference the current or parent directory")
	}
	if strings.ContainsAny(runID, `/\`) {
		return errors.New("run id must not contain path separators")
	}
	if cleaned := filepath.Clean(runID); cleaned != runID || cleaned == "." || cleaned == ".." {
		return errors.New("run id must resolve to a single safe path segment")
	}
	return nil
}
