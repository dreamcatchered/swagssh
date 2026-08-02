//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

type pipePty struct {
	stdinWriter  *os.File
	stdoutReader *os.File
	stdinCloser  *os.File
	stdoutCloser *os.File
	cmd          *exec.Cmd
}

func startPTY(cmd *exec.Cmd, rows, cols int) (*pipePty, error) {
	stdinReader, stdinWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		stdinReader.Close()
		stdinWriter.Close()
		return nil, err
	}

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

	stdinReader.Close()
	stdoutWriter.Close()

	return &pipePty{
		stdinWriter:  stdinWriter,
		stdoutReader: stdoutReader,
		stdinCloser:  stdinReader,
		stdoutCloser: stdoutWriter,
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
