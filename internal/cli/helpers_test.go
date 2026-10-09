package cli

import (
	"context"
	"io"

	"github.com/gookit/goutil/cflag/capp"
)

// newCLI builds the root command with the default (unstamped) build info.
func newCLI(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) *capp.App {
	return New(ctx, stdin, stdout, stderr, Build{Version: "dev", VersionLine: "suag dev (dev)"})
}
