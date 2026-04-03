//go:build !windows

package agentcore

import "os"

func runtimeReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
