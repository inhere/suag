//go:build windows

package acpserver

import "errors"

func mkfifo(string, uint32) error { return errors.New("fifo unsupported on windows") }
