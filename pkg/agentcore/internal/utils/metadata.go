package utils

// CloneMetadata returns a recursive copy of a metadata map.
//
// It clones nested map[string]any and []any values so callers can safely
// mutate the returned structure without affecting the source tree.
func CloneMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return CloneMetadata(val)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return v
	}
}
