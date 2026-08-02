package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func main() {
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
	fmt.Fprintf(os.Stderr, "  swagssh share [--server HOST:PORT]           Share terminal (creates session ID)\n")
	fmt.Fprintf(os.Stderr, "  swagssh connect [--server HOST:PORT] <ID>    Connect to shared session\n")
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

	bannerBuf := make([]byte, 4096)
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

	go func() {
		sigCh := make(chan os.Signal, 1)
		notifyWinch(sigCh)
		for range sigCh {
		}
	}()

	errCh := make(chan error, 3)

	go func() {
		_, e := io.Copy(channel, ptyRW)
		errCh <- e
	}()

	go func() {
		_, e := io.Copy(ptyRW, channel)
		errCh <- e
	}()

	go func() {
		errCh <- cmd.Wait()
	}()

	<-errCh
}

func runConnect(serverAddr, sessionID string) {
	config := &ssh.ClientConfig{
		User:            sessionID,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Auth:            []ssh.AuthMethod{ssh.Password("connect")},
		Timeout:         30 * 1000000000,
	}

	client, err := ssh.Dial("tcp", serverAddr, config)
	if err != nil {
		log.Fatalf("Failed to connect to session %s: %v", sessionID, err)
	}
	defer client.Close()

	channel, requests, err := client.OpenChannel("session", nil)
	if err != nil {
		log.Fatalf("Failed to open channel: %v", err)
	}
	defer channel.Close()

	go ssh.DiscardRequests(requests)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		log.Fatal("This command must be run from an interactive terminal")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		log.Fatalf("Failed to set raw mode: %v", err)
	}
	defer term.Restore(fd, oldState)

	w, h, _ := term.GetSize(fd)

	go func() {
		sigCh := make(chan os.Signal, 1)
		notifyWinch(sigCh)
		for range sigCh {
			nw, nh, err := term.GetSize(fd)
			if err != nil {
				continue
			}
			payload := make([]byte, 16)
			binary.BigEndian.PutUint32(payload[0:4], uint32(nw))
			binary.BigEndian.PutUint32(payload[4:8], uint32(nh))
			binary.BigEndian.PutUint32(payload[8:12], 0)
			binary.BigEndian.PutUint32(payload[12:16], 0)
			_, _, _ = client.SendRequest("window-change", false, payload)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Restore(fd, oldState)
		os.Exit(0)
	}()

	if w > 0 && h > 0 {
		payload := make([]byte, 16)
		binary.BigEndian.PutUint32(payload[0:4], uint32(w))
		binary.BigEndian.PutUint32(payload[4:8], uint32(h))
		binary.BigEndian.PutUint32(payload[8:12], 0)
		binary.BigEndian.PutUint32(payload[12:16], 0)
		_, _, _ = client.SendRequest("window-change", false, payload)
	}

	errCh := make(chan error, 2)

	go func() {
		_, e := io.Copy(channel, os.Stdin)
		errCh <- e
	}()

	go func() {
		_, e := io.Copy(os.Stdout, channel)
		errCh <- e
	}()

	<-errCh
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
