// Command keyprobe prints what the terminal sends for each key, in raw mode.
//
// It settles questions about a terminal's key encoding - whether Ctrl+Enter reaches
// the program as a sequence of its own, whether Shift+Enter differs from Enter, what
// an arrow key looks like - without guessing from documentation: run it in the
// terminal, press the keys, read the bytes.
//
//	go run ./scripts/keyprobe
//
// Ctrl+C or Ctrl+D leaves it. Everything after that is printed as hex and as a
// quoted string, so a terminal that sends a control byte, an escape sequence or a
// Windows Terminal win32-input-mode record is all readable at a glance.
package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func main() {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Printf("MakeRaw: %v\r\n", err)
		os.Exit(1)
	}
	defer func() { _ = term.Restore(fd, old) }()

	width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
	fmt.Printf("stdin is a terminal: %v\r\n", term.IsTerminal(fd))
	fmt.Printf("stdout size: %dx%d (err=%v)\r\n", width, height, sizeErr)
	fmt.Print("press keys (Ctrl+C or Ctrl+D quits):\r\n")

	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			fmt.Printf("read: %v\r\n", err)
			return
		}
		got := buf[:n]
		fmt.Printf("read %2d byte(s): % x   %q\r\n", n, got, string(got))
		if n == 1 && (got[0] == 0x03 || got[0] == 0x04) { // Ctrl+C, Ctrl+D
			return
		}
	}
}
