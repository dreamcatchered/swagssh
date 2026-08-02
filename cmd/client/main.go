package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/crypto/ssh"
)

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		usage()
	}

	command := os.Args[1]
	args := os.Args[2:]

	serverAddr := "ssh.swag.best:2222"

	var filtered []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--server" && i+1 < len(args) {
			serverAddr = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "--server=") {
			serverAddr = strings.TrimPrefix(args[i], "--server=")
		} else {
			filtered = append(filtered, args[i])
		}
	}

	switch command {
	case "share":
		runShare(serverAddr)
	case "connect":
		if len(filtered) == 0 {
			log.Fatal("Session ID required: swagssh connect <session-id>")
		}
		runConnect(serverAddr, filtered[0])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "swagSSH - Instant Reverse SSH\n\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  swagssh share [--server HOST:PORT]\n")
	fmt.Fprintf(os.Stderr, "  swagssh connect [--server HOST:PORT] <session-id>\n")
	os.Exit(1)
}

func runShare(serverAddr string) {
	config := &ssh.ClientConfig{
		User:            "share",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Auth:            []ssh.AuthMethod{ssh.Password("share")},
		Timeout:         30 * 1000000000,
	}

	client, err := ssh.Dial("tcp", serverAddr, config)
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", serverAddr, err)
	}
	defer client.Close()

	channel, requests, err := client.OpenChannel("session", nil)
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	go ssh.DiscardRequests(requests)

	bannerBuf := make([]byte, 8192)
	n, _ := channel.Read(bannerBuf)
	if n > 0 {
		fmt.Fprint(os.Stderr, string(bannerBuf[:n]))
	}

	osShell := getShell()
	cmd := exec.Command(osShell[0], osShell[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptyRW, err := startPTY(cmd, 40, 120)
	if err != nil {
		log.Fatalf("Failed to start PTY: %v", err)
	}
	defer ptyRW.Close()

	errCh := make(chan error, 3)

	go func() {
		n, e := io.Copy(channel, ptyRW)
		if e != nil && e != io.EOF {
			log.Printf("[share] pty->channel closed after %d bytes: %v", n, e)
		}
		errCh <- e
	}()

	go func() {
		n, e := io.Copy(ptyRW, channel)
		if e != nil && e != io.EOF {
			log.Printf("[share] channel->pty closed after %d bytes: %v", n, e)
		}
		errCh <- e
	}()

	go func() {
		errCh <- cmd.Wait()
	}()

	err = <-errCh
	if err != nil && err != io.EOF {
		log.Printf("[share] session ended: %v", err)
	}
}

func getShell() []string {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		return []string{comspec, "/k"}
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		if _, err := os.Stat("/bin/bash"); err == nil {
			shell = "/bin/bash"
		} else if _, err := os.Stat("/bin/sh"); err == nil {
			shell = "/bin/sh"
		}
	}
	if shell == "" {
		return []string{"/bin/sh"}
	}
	return []string{shell}
}
