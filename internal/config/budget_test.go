package axconfig

import (
	"testing"

	"github.com/gookit/goutil/x/assert"
)

func loadBudgetYAML(t *testing.T, doc string) {
	t.Helper()
	loadYAML(t, doc)
	for _, key := range []string{
		"SUGA_BUDGET_TOKENS", "SUGA_BUDGET_COST_USD", "SUGA_BUDGET_SCOPE",
		"SUGA_BUDGET_ESTIMATE", "SUGA_BUDGET_ESTIMATE_FACTOR", "SUGA_BUDGET_NOTICE_RATIO",
	} {
		t.Setenv(key, "")
	}
}

func TestReadBudgetSettings(t *testing.T) {
	t.Run("unconfigured returns the zero value", func(t *testing.T) {
		loadBudgetYAML(t, "")
		settings, err := ReadBudgetSettings(BudgetFlags{})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, BudgetSettings{}, settings)
	})
	t.Run("config reads all settings and applies defaults", func(t *testing.T) {
		loadBudgetYAML(t, "budget:\n  tokens: 200\n  cost_usd: 1.25\n")
		settings, err := ReadBudgetSettings(BudgetFlags{})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 200, settings.Tokens)
		assert.Eq(t, 1.25, settings.CostUSD)
		assert.Eq(t, DefaultBudgetScope, settings.Scope)
		assert.Eq(t, DefaultBudgetEstimate, settings.Estimate)
		assert.Eq(t, DefaultBudgetEstimateFactor, settings.EstimateFactor)
		assert.Eq(t, DefaultBudgetNoticeRatio, settings.NoticeRatio)
	})
	t.Run("config accepts explicit scope and estimate settings", func(t *testing.T) {
		loadBudgetYAML(t, "budget:\n  tokens: 200\n  scope: SESSION\n  estimate: OFF\n  estimate_factor: 1.5\n  notice_ratio: 0\n")
		settings, err := ReadBudgetSettings(BudgetFlags{})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, "session", settings.Scope)
		assert.Eq(t, "off", settings.Estimate)
		assert.Eq(t, 1.5, settings.EstimateFactor)
		assert.Eq(t, float64(0), settings.NoticeRatio)
	})
	t.Run("flag overrides environment and config", func(t *testing.T) {
		loadBudgetYAML(t, "budget:\n  tokens: 7\n  cost_usd: 1\n  scope: turn\n")
		t.Setenv("SUGA_BUDGET_TOKENS", "3")
		t.Setenv("SUGA_BUDGET_COST_USD", "2.5")
		t.Setenv("SUGA_BUDGET_SCOPE", "SESSION")
		settings, err := ReadBudgetSettings(BudgetFlags{Tokens: 2, CostUSD: 1.5, Scope: "turn"})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 2, settings.Tokens)
		assert.Eq(t, 1.5, settings.CostUSD)
		assert.Eq(t, "turn", settings.Scope)
	})
	t.Run("environment overrides config including non-flag settings", func(t *testing.T) {
		loadBudgetYAML(t, "budget:\n  tokens: 7\n  cost_usd: 1\n  scope: turn\n  estimate: auto\n  estimate_factor: 1\n  notice_ratio: 0.8\n")
		t.Setenv("SUGA_BUDGET_TOKENS", "3")
		t.Setenv("SUGA_BUDGET_COST_USD", "2.5")
		t.Setenv("SUGA_BUDGET_SCOPE", "SESSION")
		t.Setenv("SUGA_BUDGET_ESTIMATE", "OFF")
		t.Setenv("SUGA_BUDGET_ESTIMATE_FACTOR", "2")
		t.Setenv("SUGA_BUDGET_NOTICE_RATIO", "0")
		settings, err := ReadBudgetSettings(BudgetFlags{})
		assert.Require(t, assert.NoErr(t, err))
		assert.Eq(t, 3, settings.Tokens)
		assert.Eq(t, 2.5, settings.CostUSD)
		assert.Eq(t, "session", settings.Scope)
		assert.Eq(t, "off", settings.Estimate)
		assert.Eq(t, float64(2), settings.EstimateFactor)
		assert.Eq(t, float64(0), settings.NoticeRatio)
	})

	for _, tc := range []struct {
		name  string
		setup func(*testing.T)
		flags BudgetFlags
		want  string
	}{
		{name: "negative token flag", flags: BudgetFlags{Tokens: -1}, want: "--budget-tokens"},
		{name: "negative cost flag", flags: BudgetFlags{CostUSD: -1}, want: "--budget-cost-usd"},
		{name: "invalid token environment", setup: func(t *testing.T) { t.Setenv("SUGA_BUDGET_TOKENS", "bad") }, want: "SUGA_BUDGET_TOKENS"},
		{name: "negative cost environment", setup: func(t *testing.T) { t.Setenv("SUGA_BUDGET_COST_USD", "-0.1") }, want: "SUGA_BUDGET_COST_USD"},
		{name: "invalid scope environment", setup: func(t *testing.T) { t.Setenv("SUGA_BUDGET_SCOPE", "process") }, want: "SUGA_BUDGET_SCOPE"},
		{name: "invalid estimate config", setup: func(t *testing.T) { loadBudgetYAML(t, "budget:\n  tokens: 1\n  estimate: sometimes\n") }, want: "budget.estimate"},
		{name: "non-positive factor config", setup: func(t *testing.T) { loadBudgetYAML(t, "budget:\n  tokens: 1\n  estimate_factor: 0\n") }, want: "budget.estimate_factor"},
		{name: "negative notice ratio config", setup: func(t *testing.T) { loadBudgetYAML(t, "budget:\n  tokens: 1\n  notice_ratio: -0.1\n") }, want: "budget.notice_ratio"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup == nil {
				loadBudgetYAML(t, "")
			} else {
				tc.setup(t)
			}
			_, err := ReadBudgetSettings(tc.flags)
			assert.Require(t, assert.Err(t, err))
			assert.StrContains(t, err.Error(), tc.want)
		})
	}
}
