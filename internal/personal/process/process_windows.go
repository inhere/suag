//go:build windows

package process

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const processStillActive = 259

func processExists(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Only ERROR_INVALID_PARAMETER proves that the PID does not exist.
		return !processErrorMeansDead(err)
	}
	var exitCode uint32
	queryErr := windows.GetExitCodeProcess(handle, &exitCode)
	_ = windows.CloseHandle(handle)
	if queryErr != nil {
		return true
	}
	return exitCode == processStillActive
}

func processErrorMeansDead(err error) bool {
	return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

func validateSocketPath(path string) error {
	if len(utf16.Encode([]rune(path))) > 100 {
		return fmt.Errorf("local socket path exceeds Windows AF_UNIX limit (100 UTF-16 code units): %s", path)
	}
	return nil
}

// secureDir replaces dir's DACL with a protected (non-inherited) one granting full control
// only to the current user and SYSTEM. os.Chmod does not touch ACLs on Windows, and an
// AF_UNIX socket otherwise inherits the parent's ACL (e.g. a shared C:\suag grants
// Authenticated Users access). Administrators are deliberately not listed: they can take
// ownership regardless, and an explicit ACE would only widen the surface. SYSTEM is kept
// so backup/AV services do not break. The ACEs are inheritable so the socket, record and
// lock files created inside get the same restriction.
func secureDir(dir string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("query current user: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("build SYSTEM SID: %w", err)
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, 2)
	for _, sid := range []*windows.SID{user.User.Sid, system} {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build ACL: %w", err)
	}
	// PROTECTED stops inheritance from the parent; existing inherited ACEs are dropped.
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}
