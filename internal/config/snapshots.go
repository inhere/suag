package axconfig

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	gkconfig "github.com/gookit/config/v2"
)

const (
	// DefaultSnapshotMaxFileBytes is the largest before-image retained for one call.
	DefaultSnapshotMaxFileBytes int64 = 16 << 20
	// DefaultSnapshotMaxSessionBytes bounds before-images in one session.
	DefaultSnapshotMaxSessionBytes int64 = 256 << 20
	// DefaultSnapshotMaxGlobalBytes bounds before-images below the sessions root.
	DefaultSnapshotMaxGlobalBytes int64 = 1 << 30
	DefaultSnapshotKeepTurns            = 20
	DefaultSnapshotKeepDays             = 7
	SnapshotOnErrorDeny                 = "deny"
	SnapshotOnErrorContinue             = "continue"
)

// SnapshotSettings contains the file-tool checkpoint policy. The settings are
// kept in config rather than personal so wiring can use one validated policy
// for run, chat and ACP without making the storage layer read global config.
type SnapshotSettings struct {
	Enabled         bool
	MaxFileBytes    int64
	MaxSessionBytes int64
	MaxGlobalBytes  int64
	KeepTurns       int
	KeepDays        int
	OnError         string
}

// DefaultSnapshotSettings returns the secure checkpoint policy. A caller can
// explicitly turn snapshots off without removing the undo command surface.
func DefaultSnapshotSettings() SnapshotSettings {
	return SnapshotSettings{
		Enabled:         true,
		MaxFileBytes:    DefaultSnapshotMaxFileBytes,
		MaxSessionBytes: DefaultSnapshotMaxSessionBytes,
		MaxGlobalBytes:  DefaultSnapshotMaxGlobalBytes,
		KeepTurns:       DefaultSnapshotKeepTurns,
		KeepDays:        DefaultSnapshotKeepDays,
		OnError:         SnapshotOnErrorDeny,
	}
}

// ReadSnapshotSettings reads and validates tools.snapshots. Unknown fields are
// rejected so a misspelled limit cannot silently weaken the rollback contract.
func ReadSnapshotSettings() (SnapshotSettings, error) {
	settings := DefaultSnapshotSettings()
	raw := gkconfig.Get("tools.snapshots")
	if raw == nil {
		return settings, nil
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return settings, fmt.Errorf("tools.snapshots must be an object")
	}
	for key := range fields {
		switch key {
		case "enabled", "max_file_bytes", "max_session_bytes", "max_global_bytes", "keep_turns", "keep_days", "on_error":
		default:
			return settings, fmt.Errorf("tools.snapshots.%s is not a recognized setting", key)
		}
	}
	if value, exists := fields["enabled"]; exists {
		parsed, ok := value.(bool)
		if !ok {
			return settings, fmt.Errorf("tools.snapshots.enabled must be a boolean")
		}
		settings.Enabled = parsed
	}
	for key, dst := range map[string]*int64{
		"max_file_bytes":    &settings.MaxFileBytes,
		"max_session_bytes": &settings.MaxSessionBytes,
		"max_global_bytes":  &settings.MaxGlobalBytes,
	} {
		value, exists := fields[key]
		if !exists {
			continue
		}
		parsed, ok := snapshotInteger(value)
		if !ok || parsed < 1 {
			return settings, fmt.Errorf("tools.snapshots.%s must be a whole number >= 1", key)
		}
		*dst = parsed
	}
	for key, dst := range map[string]*int{
		"keep_turns": &settings.KeepTurns,
		"keep_days":  &settings.KeepDays,
	} {
		value, exists := fields[key]
		if !exists {
			continue
		}
		parsed, ok := snapshotInteger(value)
		if !ok || parsed < 1 || int64(int(parsed)) != parsed {
			return settings, fmt.Errorf("tools.snapshots.%s must be a whole number >= 1", key)
		}
		*dst = int(parsed)
	}
	if value, exists := fields["on_error"]; exists {
		parsed, ok := value.(string)
		if !ok {
			return settings, fmt.Errorf("tools.snapshots.on_error must be deny or continue")
		}
		switch strings.ToLower(strings.TrimSpace(parsed)) {
		case SnapshotOnErrorDeny, SnapshotOnErrorContinue:
			settings.OnError = strings.ToLower(strings.TrimSpace(parsed))
		default:
			return settings, fmt.Errorf("tools.snapshots.on_error must be deny or continue")
		}
	}
	return settings, nil
}

func snapshotInteger(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), int64(int(value)) == int64(value)
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		return int64(value), uint64(int64(value)) == uint64(value)
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), uint64(int64(value)) == uint64(value)
	case uint64:
		return int64(value), uint64(int64(value)) == value
	case float64:
		return int64(value), value >= math.MinInt64 && value <= math.MaxInt64 && value == math.Trunc(value)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
