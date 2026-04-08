package agentcore

import "github.com/inhere/axon/pkg/agentcore/files"

// FileStorage is kept as a compatibility alias for the first-party filesystem storage.
type FileStorage = files.Storage

// NewFileStorage is kept as a compatibility constructor for the first-party filesystem storage.
func NewFileStorage(root string) *FileStorage {
	return files.NewStorage(root)
}
