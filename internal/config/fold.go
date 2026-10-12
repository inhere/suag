package axconfig

import (
	"fmt"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// History folding defaults (docs/design/2026-10-12-history-folding-design.md,
// Q1: on, every 10 steps, keep the last 5 whole).
const (
	DefaultFoldEvery = 10
	DefaultFoldKeep  = 5
)

// FoldSettings configures history folding of the request view.
type FoldSettings struct {
	// Every (context.fold_every) is the batch size the fold boundary moves
	// by; 0 turns folding off.
	Every int
	// Keep (context.fold_keep) is how many recent steps stay whole (>= 1).
	Keep int
}

// ReadFoldSettings reads context.fold_every (>= 0) and context.fold_keep
// (>= 1). Unset keys take the defaults; an invalid value is an error.
func ReadFoldSettings() (FoldSettings, error) {
	out := FoldSettings{Every: DefaultFoldEvery, Keep: DefaultFoldKeep}
	read := func(key string, min int, dst *int) error {
		if gkconfig.Get(key) == nil {
			return nil
		}
		raw := strings.TrimSpace(gkconfig.String(key))
		n, err := strconv.Atoi(raw)
		if err != nil || n < min {
			return fmt.Errorf("%s must be an integer >= %d, got %q", key, min, raw)
		}
		*dst = n
		return nil
	}
	if err := read("context.fold_every", 0, &out.Every); err != nil {
		return FoldSettings{}, err
	}
	if err := read("context.fold_keep", 1, &out.Keep); err != nil {
		return FoldSettings{}, err
	}
	return out, nil
}
