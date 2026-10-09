package tools

import (
	"context"
	"testing"

	"github.com/gookit/goutil/x/assert"
)

type answerAsker string

func (a answerAsker) Ask(context.Context, Question) (string, error) { return string(a), nil }

func TestBindableAsker(t *testing.T) {
	t.Run("unbound cannot ask", func(t *testing.T) {
		_, err := NewBindableAsker().Ask(context.Background(), Question{Tool: "shell_exec"})
		assert.ErrIs(t, err, ErrCannotAsk)
		var nilAsker *BindableAsker
		_, err = nilAsker.Ask(context.Background(), Question{})
		assert.ErrIs(t, err, ErrCannotAsk)
	})
	t.Run("bound forwards and unbinds", func(t *testing.T) {
		asker := NewBindableAsker()
		asker.Bind(answerAsker("y"))
		answer, err := asker.Ask(context.Background(), Question{})
		assert.NoErr(t, err)
		assert.Eq(t, "y", answer)
		asker.Bind(nil)
		_, err = asker.Ask(context.Background(), Question{})
		assert.ErrIs(t, err, ErrCannotAsk)
	})
}
