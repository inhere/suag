//go:build windows

package process

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsProcessErrorRequiresProofOfMissingPID(t *testing.T) {
	if processErrorMeansDead(windows.ERROR_ACCESS_DENIED) {
		t.Fatal("access denied must not classify a process as dead")
	}
	if !processErrorMeansDead(windows.ERROR_INVALID_PARAMETER) {
		t.Fatal("invalid PID must classify the process as dead")
	}
	if !processExists(4) {
		t.Fatal("Windows System PID 4 must remain conservatively alive")
	}
}
