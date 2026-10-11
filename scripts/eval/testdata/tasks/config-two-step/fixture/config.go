package config

import (
	"os"
	"strconv"
)

// Port returns the listen port from APP_PORT, defaulting to 8080 when unset.
func Port() int {
	raw := os.Getenv("APP_PORT")
	if raw == "" {
		return 8080
	}
	v, _ := strconv.Atoi(raw)
	return v
}

// Host returns the listen host from APP_HOST, defaulting to localhost.
func Host() string {
	if v := os.Getenv("APP_HOST"); v != "" {
		return v
	}
	return "localhost"
}

// Debug reports whether APP_DEBUG is set to a non-empty value.
func Debug() bool {
	return os.Getenv("APP_DEBUG") != ""
}
