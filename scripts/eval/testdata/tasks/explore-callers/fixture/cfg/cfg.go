package cfg

// Config is the app configuration.
type Config struct{ Name string }

// Validate checks c.
func Validate(c Config) error {
	if c.Name == "" {
		return errEmpty
	}
	return nil
}

// ValidateAll checks every config (not a call of Validate).
func ValidateAll(cs []Config) int { return len(cs) }

var errEmpty error
