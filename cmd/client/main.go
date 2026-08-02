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

func runConnect(serverAddr, sessionID string) {
	config := &ssh.ClientConfig{
		User:            sessionID,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Auth:            []ssh.AuthMethod{ssh.Password("connect")},
		Timeout:         30 * 1000000000,
	}

	client, err := ssh.Dial("tcp", serverAddr, config)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	channel, requests, err := client.OpenChannel("session", nil)
	if err != nil {
		log.Fatalf("Session not found or already closed: %v", err)
	}
	defer channel.Close()

	go ssh.DiscardRequests(requests)

	fd := int(os.Stdin.Fd())
	istty := term.IsTerminal(fd)
	var oldState *term.State

	if istty {
		var err error
		oldState, err = term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, oldState)
		}

		w, h, _ := term.GetSize(fd)
		if w > 0 && h > 0 {
			payload := make([]byte, 16)
			binary.BigEndian.PutUint32(payload[0:4], uint32(w))
			binary.BigEndian.PutUint32(payload[4:8], uint32(h))
			binary.BigEndian.PutUint32(payload[8:12], 0)
			binary.BigEndian.PutUint32(payload[12:16], 0)
			_, _, _ = client.SendRequest("window-change", false, payload)
		}

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
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if istty && oldState != nil {
			term.Restore(fd, oldState)
		}
		os.Exit(0)
	}()

	errCh := make(chan error, 2)

	go func() {
		_, e := io.Copy(channel, os.Stdin)
		errCh <- e
	}()

	go func() {
		_, e := io.Copy(os.Stdout, channel)
		errCh <- e
	}()

	e := <-errCh
	if e != nil && e != io.EOF {
		log.Printf("[connect] connection ended: %v", e)
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
