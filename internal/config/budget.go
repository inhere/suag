package axconfig

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

const (
	// DefaultBudgetScope is the scope used when a budget limit is enabled but
	// no scope is configured.
	DefaultBudgetScope = "turn"
	// DefaultBudgetEstimate controls provider usage estimation for a budget.
	DefaultBudgetEstimate = "auto"
	// DefaultBudgetEstimateFactor is the multiplier applied by the estimator.
	DefaultBudgetEstimateFactor = 1.0
	// DefaultBudgetNoticeRatio is the chat reminder threshold.
	DefaultBudgetNoticeRatio = 0.8
)

// BudgetFlags are the command-line budget overrides. A zero numeric value and
// an empty scope mean that the corresponding environment/config value applies.
// Estimate settings intentionally have no CLI flags in this version.
type BudgetFlags struct {
	Tokens  int
	CostUSD float64
	Scope   string
}

// BudgetSettings configures the optional turn/session token and cost budget.
// A zero-value BudgetSettings means that no budget is enabled.
type BudgetSettings struct {
	Tokens         int
	CostUSD        float64
	Scope          string
	Estimate       string
	EstimateFactor float64
	NoticeRatio    float64
}

// ReadBudgetSettings resolves budget flags, environment overrides and config.
// Limits use flag > environment > config precedence. The remaining settings
// use their config defaults, with matching SUGA_BUDGET_* environment overrides;
// every explicitly supplied invalid value fails closed at startup.
func ReadBudgetSettings(flags BudgetFlags) (BudgetSettings, error) {
	tokens, err := readBudgetInt(flags.Tokens, "--budget-tokens", "SUGA_BUDGET_TOKENS", "budget.tokens")
	if err != nil {
		return BudgetSettings{}, err
	}
	cost, err := readBudgetFloat(flags.CostUSD, "--budget-cost-usd", "SUGA_BUDGET_COST_USD", "budget.cost_usd")
	if err != nil {
		return BudgetSettings{}, err
	}
	scope, err := readBudgetEnum(flags.Scope, "--budget-scope", "SUGA_BUDGET_SCOPE", "budget.scope", "turn", "session")
	if err != nil {
		return BudgetSettings{}, err
	}
	estimate, err := readBudgetEnum("", "", "SUGA_BUDGET_ESTIMATE", "budget.estimate", "auto", "on", "off")
	if err != nil {
		return BudgetSettings{}, err
	}
	factor, err := readBudgetPositiveFloat("SUGA_BUDGET_ESTIMATE_FACTOR", "budget.estimate_factor", DefaultBudgetEstimateFactor)
	if err != nil {
		return BudgetSettings{}, err
	}
	noticeRatio, err := readBudgetNonNegativeFloat("SUGA_BUDGET_NOTICE_RATIO", "budget.notice_ratio", DefaultBudgetNoticeRatio)
	if err != nil {
		return BudgetSettings{}, err
	}

	// Keep the complete zero value when neither limit is enabled. This is what
	// preserves all pre-budget behavior and lets later layers test enablement
	// without having to know the defaults above.
	if tokens == 0 && cost == 0 {
		return BudgetSettings{}, nil
	}
	if scope == "" {
		scope = DefaultBudgetScope
	}
	if estimate == "" {
		estimate = DefaultBudgetEstimate
	}
	return BudgetSettings{
		Tokens: tokens, CostUSD: cost, Scope: scope, Estimate: estimate,
		EstimateFactor: factor, NoticeRatio: noticeRatio,
	}, nil
}

func readBudgetInt(flagValue int, flagName, envName, configKey string) (int, error) {
	if flagValue < 0 {
		return 0, fmt.Errorf("%s must be a whole number >= 0, got %d", flagName, flagValue)
	}
	if flagValue > 0 {
		return flagValue, nil
	}
	if raw, ok := budgetEnv(envName); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%s must be a whole number >= 0, got %q", envName, raw)
		}
		return n, nil
	}
	if raw, ok := budgetConfig(configKey); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%s must be a whole number >= 0, got %q", configKey, raw)
		}
		return n, nil
	}
	return 0, nil
}

func readBudgetFloat(flagValue float64, flagName, envName, configKey string) (float64, error) {
	if flagValue < 0 || math.IsNaN(flagValue) || math.IsInf(flagValue, 0) {
		return 0, fmt.Errorf("%s must be a number >= 0, got %v", flagName, flagValue)
	}
	if flagValue > 0 {
		return flagValue, nil
	}
	if raw, ok := budgetEnv(envName); ok {
		return parseBudgetFloat(envName, raw, false)
	}
	if raw, ok := budgetConfig(configKey); ok {
		return parseBudgetFloat(configKey, raw, false)
	}
	return 0, nil
}

func readBudgetPositiveFloat(envName, configKey string, defaultValue float64) (float64, error) {
	raw, ok := budgetEnv(envName)
	key := envName
	if !ok {
		raw, ok = budgetConfig(configKey)
		key = configKey
	}
	if !ok {
		return defaultValue, nil
	}
	return parseBudgetFloat(key, raw, true)
}

func readBudgetNonNegativeFloat(envName, configKey string, defaultValue float64) (float64, error) {
	raw, ok := budgetEnv(envName)
	key := envName
	if !ok {
		raw, ok = budgetConfig(configKey)
		key = configKey
	}
	if !ok {
		return defaultValue, nil
	}
	return parseBudgetFloat(key, raw, false)
}

func parseBudgetFloat(key, raw string, positive bool) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	invalid := err != nil || math.IsNaN(value) || math.IsInf(value, 0)
	if positive {
		invalid = invalid || value <= 0
	} else {
		invalid = invalid || value < 0
	}
	if invalid {
		operator := ">= 0"
		if positive {
			operator = "> 0"
		}
		return 0, fmt.Errorf("%s must be a number %s, got %q", key, operator, raw)
	}
	return value, nil
}

func readBudgetEnum(flagValue, flagName, envName, configKey string, allowed ...string) (string, error) {
	value := strings.TrimSpace(flagValue)
	key := flagName
	if value == "" {
		if env, ok := budgetEnv(envName); ok {
			value, key = env, envName
		} else if config, ok := budgetConfig(configKey); ok {
			value, key = config, configKey
		} else {
			return "", nil
		}
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", fmt.Errorf("%s must be one of %s, got %q", key, strings.Join(allowed, ", "), value)
}

func budgetEnv(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	value = strings.TrimSpace(value)
	return value, ok && value != ""
}

func budgetConfig(key string) (string, bool) {
	if gkconfig.Get(key) == nil {
		return "", false
	}
	return strings.TrimSpace(gkconfig.String(key)), true
}
