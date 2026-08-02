//go:build windows

package main

import (
	"io"
	"os/exec"
	"syscall"
)

type pipePty struct {
	stdinWriter  *io.PipeWriter
	stdoutReader *io.PipeReader
	cmd          *exec.Cmd
}

func startPTY(cmd *exec.Cmd, rows, cols int) (*pipePty, error) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	cmd.Stdin = stdinReader
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stdoutWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := cmd.Start(); err != nil {
		stdinReader.Close()
		stdinWriter.Close()
		stdoutReader.Close()
		stdoutWriter.Close()
		return nil, err
	}

	return &pipePty{
		stdinWriter:  stdinWriter,
		stdoutReader: stdoutReader,
		cmd:          cmd,
	}, nil
}

func (p *pipePty) Read(buf []byte) (int, error) {
	return p.stdoutReader.Read(buf)
}

func (p *pipePty) Write(buf []byte) (int, error) {
	return p.stdinWriter.Write(buf)
}

func (p *pipePty) Close() error {
	p.stdinWriter.Close()
	p.stdoutReader.Close()
	return nil
}

func setPTYSize(p *pipePty, rows, cols int) error {
	return nil
}
