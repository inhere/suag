package agentcoretesting

import (
	stdtesting "testing"

	"github.com/inhere/axon/pkg/agentcore"
	"github.com/inhere/axon/pkg/agentcore/files"
)

func TestRunStorageSuite_WithFilesStorage(t *stdtesting.T) {
	RunStorageSuite(t, NewStorageFactoryFromPath(func(t *stdtesting.T, root string) agentcore.Storage {
		t.Helper()
		return files.NewStorage(root)
	}))
}
