package agentcore_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestLayeringNoAppImports keeps pkg/agentcore independent of the suga
// application: no package under pkg/agentcore, tests included, may depend on
// another github.com/inhere/suga package.
func TestLayeringNoAppImports(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("layering guard needs the go command: %v", err)
	}
	// The test runs in the pkg/agentcore directory, so ./... is the module subtree.
	cmd := exec.Command(goBin, "list", "-deps", "-test", "-f", "{{.ImportPath}}", "./...")
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("go list failed: %v\n%s", err, stderr)
	}
	const module, allowed = "github.com/inhere/suga/", "github.com/inhere/suga/pkg/agentcore"
	var leaks []string
	for _, path := range strings.Fields(string(out)) {
		// go list -test also prints test variants such as "pkg [pkg.test]".
		if strings.HasPrefix(path, module) && !strings.HasPrefix(path, allowed) {
			leaks = append(leaks, path)
		}
	}
	if len(leaks) > 0 {
		t.Fatalf("pkg/agentcore must not depend on the suga application; found:\n  %s", strings.Join(leaks, "\n  "))
	}
}
