package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gookit/goutil/cflag/capp"
	personalprocess "github.com/inhere/suag/internal/personal/process"
)

// maxSendStdinBytes caps `suag send` stdin; exit codes: 2 ambiguous, 3 offline, 4 too large.
const maxSendStdinBytes = 1 << 20

func newSendCommand(stdin io.Reader, stderr io.Writer) *capp.Cmd {
	var sendSession, sendText string
	var sendWait bool
	send := capp.NewCmd("send", "Send text to a local Suag chat process", func(c *capp.Cmd) error {
		if len(c.RemainArgs()) != 0 {
			return fmt.Errorf("suag send does not accept arguments")
		}
		if strings.TrimSpace(sendSession) == "" {
			return fmt.Errorf("--session is required")
		}
		text := sendText
		if text == "" {
			// Read one byte past the cap so oversize input is rejected, never truncated.
			data, err := io.ReadAll(io.LimitReader(stdin, maxSendStdinBytes+1))
			if err != nil {
				return err
			}
			if len(data) > maxSendStdinBytes {
				return processExitError{code: 4, err: fmt.Errorf("stdin exceeds the %d byte (1 MiB) limit; nothing was sent", maxSendStdinBytes)}
			}
			text = string(data)
		}
		root, err := localProcessRoot()
		if err != nil {
			return err
		}
		records, err := personalprocess.List(root)
		if err != nil {
			return err
		}
		record, err := personalprocess.Resolve(records, sendSession)
		if errors.Is(err, personalprocess.ErrNotOnline) {
			return processExitError{code: 3, err: err}
		}
		if errors.Is(err, personalprocess.ErrAmbiguous) {
			return processExitError{code: 2, err: err}
		}
		if err != nil {
			return err
		}
		// --wait keeps the connection until a queued message actually starts a turn (or is
		// discarded, exit 3). Without it "queued" returns at once, so an automated
		// sender never blocks for the length of the chat's current turn.
		response, err := personalprocess.Send(record, personalprocess.Request{Text: text, From: "cli", Wait: sendWait})
		if errors.Is(err, personalprocess.ErrNotOnline) {
			return processExitError{code: 3, err: err}
		}
		if err != nil {
			return err
		}
		if response.Queued && !sendWait {
			fmt.Fprintln(stderr, "queued")
		} else {
			fmt.Fprintln(stderr, "delivered")
		}
		return nil
	})
	send.SetOutput(stderr)
	send.StringVar(&sendSession, "session", "", "session ID, prefix, or name")
	send.StringVar(&sendText, "text", "", "message text; defaults to stdin")
	send.BoolVar(&sendWait, "wait", false, "wait until a queued message starts a turn; exit 3 if it is discarded")
	return send
}
