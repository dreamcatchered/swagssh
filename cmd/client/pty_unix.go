//go:build !windows

package main

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func startPTY(cmd *exec.Cmd, rows, cols int) (*os.File, error) {
	winsize := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	f, err := pty.StartWithSize(cmd, winsize)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func setPTYSize(f *os.File, rows, cols int) error {
	winsize := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}
	return pty.Setsize(f, winsize)
}
