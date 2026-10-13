package axconfig

import (
	"fmt"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

// History folding defaults (docs/design/2026-10-12-history-folding-design.md).
// Folding is off by default since 2026-10-12: rebuilding the prefix after
// each fold was about a third of the uncached input in the P5 retest (see
// docs/research/2026-10-11-suga-vs-omp-loop-analysis.md), and runs compare
// more directly with omp without it. context.fold_every: 10 turns it on.
const (
	DefaultFoldEvery = 0
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
