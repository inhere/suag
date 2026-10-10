package tools

import (
	"context"
	"errors"
	"sync"
)

// ErrCannotAsk means nobody can be asked (no interactive terminal, or the
// asker is not bound yet). Approvers turn it into a refusal (fail closed).
var ErrCannotAsk = errors.New("no interactive terminal to ask for approval")

// Question is one approval prompt for the person at the terminal.
type Question struct {
	// Tool is the name of the tool to approve.
	Tool string
	// Details are the full key arguments (command, path, query), already
	// redacted and never truncated; one entry per item, may span lines.
	Details []string
	// Prompt is the line the answer is typed on.
	Prompt string
}

// Asker asks the person a question and returns the typed answer line.
// ErrCannotAsk means nobody can answer; any other error (the person pressed
// Ctrl-C, the turn was cancelled) aborts the turn.
type Asker interface {
	Ask(ctx context.Context, q Question) (string, error)
}

// BindableAsker is an Asker whose implementation is bound after the runtime is
// assembled (chat binds its terminal input once it exists). Unbound, it
// answers ErrCannotAsk, so every question is refused until a terminal is bound.
type BindableAsker struct {
	mu    sync.RWMutex
	asker Asker
}

// NewBindableAsker returns an unbound asker.
func NewBindableAsker() *BindableAsker { return &BindableAsker{} }

// Bind sets the asker that answers from now on; nil unbinds it.
func (b *BindableAsker) Bind(asker Asker) {
	b.mu.Lock()
	b.asker = asker
	b.mu.Unlock()
}

// Ask forwards to the bound asker, or returns ErrCannotAsk when none is bound.
func (b *BindableAsker) Ask(ctx context.Context, q Question) (string, error) {
	if b == nil {
		return "", ErrCannotAsk
	}
	b.mu.RLock()
	asker := b.asker
	b.mu.RUnlock()
	if asker == nil {
		return "", ErrCannotAsk
	}
	return asker.Ask(ctx, q)
}
