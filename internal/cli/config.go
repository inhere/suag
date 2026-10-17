package cli

import (
	"fmt"
	"io"

	"github.com/gookit/goutil/cflag/capp"
)

// newConfigCommand builds the configuration command group. Its leaves are
// intentionally placeholders until the config source and YAML writer tasks
// add their behavior; keeping the tree here gives each future command a stable
// capp path and help contract.
func newConfigCommand(stderr io.Writer) *capp.Cmd {
	config := capp.NewCmd("config", "Read and maintain suga configuration")
	config.LongHelp = `Configuration commands do not modify environment variables.
Use "suga config path" to see the active home, config, and env paths.`
	config.SetOutput(stderr)
	config.Add(
		newConfigLeaf(stderr, "get", "Show an effective configuration value", "key"),
		newConfigLeaf(stderr, "set", "Set a writable configuration value", "key", "value"),
		newConfigLeaf(stderr, "unset", "Remove a writable configuration value", "key"),
		newConfigLeaf(stderr, "list", "List effective configuration values"),
		newConfigLeaf(stderr, "path", "Show configuration and data paths"),
	)
	return config
}

func newConfigLeaf(stderr io.Writer, name, description string, args ...string) *capp.Cmd {
	cmd := capp.NewCmd(name, description, func(*capp.Cmd) error {
		return fmt.Errorf("suga config %s is not implemented", name)
	})
	cmd.SetOutput(stderr)
	for _, arg := range args {
		cmd.AddArg(arg, "the configuration "+arg, true)
	}
	return cmd
}
