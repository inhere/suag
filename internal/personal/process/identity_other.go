//go:build !linux && !windows

package process

// processStartID is not available on this platform; liveness falls back to the PID alone.
func processStartID(int) string { return "" }
