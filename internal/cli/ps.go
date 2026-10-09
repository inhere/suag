package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/gookit/goutil/cflag/capp"
	appconfig "github.com/inhere/suag/internal/config"
	personalprocess "github.com/inhere/suag/internal/personal/process"
)

func newPSCommand(stdout, stderr io.Writer) *capp.Cmd {
	var psJSON bool
	ps := capp.NewCmd("ps", "List local Suag chat processes", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag ps does not accept arguments")
		}
		root, err := localProcessRoot()
		if err != nil {
			return err
		}
		records, err := personalprocess.List(root)
		if err != nil {
			return err
		}
		if psJSON {
			return json.NewEncoder(stdout).Encode(records)
		}
		for _, record := range records {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%d\t%s\n", record.SessionID, record.Name, record.CWD, record.State, record.PID, record.StartedAt.Format(time.RFC3339))
		}
		return nil
	})
	ps.SetOutput(stderr)
	ps.BoolVar(&psJSON, "json", false, "write machine-readable JSON")
	return ps
}

func localProcessRoot() (string, error) {
	home, err := appconfig.ResolveSuagHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "run"), nil
}
