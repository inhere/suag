package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/gookit/goutil/x/assert"
	appconfig "github.com/inhere/suga/internal/config"
)

func TestBudgetFlagsAreSharedByRunAndChat(t *testing.T) {
	for _, mode := range []string{"run", "chat"} {
		t.Run(mode, func(t *testing.T) {
			cmd := NewPersonalCommand(context.Background(), mode, strings.NewReader(""), nil, nil)
			for _, name := range []string{"budget-tokens", "budget-cost-usd", "budget-scope"} {
				assert.True(t, cmd.Lookup(name) != nil)
			}
		})
	}
}

func TestBudgetWiringOptions(t *testing.T) {
	want := appconfig.BudgetFlags{Tokens: 12, CostUSD: 0.75, Scope: "session"}
	for _, mode := range []string{"run", "chat"} {
		t.Run(mode, func(t *testing.T) {
			opts := personalOptions{Budget: want}
			assert.Eq(t, want, opts.wiringOptions(mode).Budget)
		})
	}
}

func TestValidatePersonalOptionsRejectsNegativeBudgetFlags(t *testing.T) {
	base := personalOptions{Prompt: "x", OutputFormat: "text"}
	t.Run("tokens", func(t *testing.T) {
		base.Budget = appconfig.BudgetFlags{Tokens: -1}
		err := validatePersonalOptions("run", base)
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "--budget-tokens")
	})
	t.Run("cost", func(t *testing.T) {
		base.Budget = appconfig.BudgetFlags{CostUSD: -1}
		err := validatePersonalOptions("run", base)
		assert.Require(t, assert.Err(t, err))
		assert.StrContains(t, err.Error(), "--budget-cost-usd")
	})
}
