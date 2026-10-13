package cli

import (
	"fmt"
	"io"

	"github.com/gookit/goutil/cflag/capp"
)

func newVersionCommand(stdout, stderr io.Writer, build Build) *capp.Cmd {
	version := capp.NewCmd("version", "Print version and commit", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suga version does not accept arguments")
		}
		_, err := fmt.Fprintln(stdout, build.VersionLine)
		return err
	})
	version.SetOutput(stderr)
	return version
}
