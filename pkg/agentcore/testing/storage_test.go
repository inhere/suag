package agentcoretesting

import (
	"context"
	"errors"
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

func TestRunStorageFixtureSuite_WithErrorCases(t *stdtesting.T) {
	saveErr := errors.New("save boom")
	loadRunErr := errors.New("load run boom")
	appendErr := errors.New("append boom")
	loadMessagesErr := errors.New("load messages boom")
	missingIDErr := errors.New("missing run id")

	RunStorageFixtureSuite(t, func(t *stdtesting.T) StorageFixture {
		t.Helper()
		return StorageFixture{
			Storage: &storageErrorStub{
				saveRunErr:      saveErr,
				loadRunErr:      loadRunErr,
				appendErr:       appendErr,
				loadMessagesErr: loadMessagesErr,
				missingIDErr:    missingIDErr,
			},
			RoundTripStorage: NewStorageFactoryFromPath(func(t *stdtesting.T, root string) agentcore.Storage {
				t.Helper()
				return files.NewStorage(root)
			})(t),
			MessagesStorage: NewStorageFactoryFromPath(func(t *stdtesting.T, root string) agentcore.Storage {
				t.Helper()
				return files.NewStorage(root)
			})(t),
			ResumeStorage: NewStorageFactoryFromPath(func(t *stdtesting.T, root string) agentcore.Storage {
				t.Helper()
				return files.NewStorage(root)
			})(t),
		}.WithSaveRunError(saveErr).
			WithLoadRunError(loadRunErr).
			WithAppendMessagesError(appendErr).
			WithLoadMessagesError(loadMessagesErr).
			WithSaveRunMissingIDError(missingIDErr)
	})
}

type storageErrorStub struct {
	saveRunErr      error
	loadRunErr      error
	appendErr       error
	loadMessagesErr error
	missingIDErr    error
}

func (s *storageErrorStub) LoadRun(context.Context, string) (*agentcore.RunState, error) {
	return nil, s.loadRunErr
}

func (s *storageErrorStub) LoadMessages(context.Context, string) ([]agentcore.Message, error) {
	return nil, s.loadMessagesErr
}

func (s *storageErrorStub) SaveRun(_ context.Context, state *agentcore.RunState) error {
	if state == nil || state.RunID == "" {
		return s.missingIDErr
	}
	return s.saveRunErr
}

func (s *storageErrorStub) AppendMessages(context.Context, string, ...agentcore.Message) error {
	return s.appendErr
}
