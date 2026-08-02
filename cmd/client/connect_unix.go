//go:build !windows

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

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
		log.Fatalf("Session not found or closed: %v", err)
	}
	defer channel.Close()

	go ssh.DiscardRequests(requests)

	fd := int(os.Stdin.Fd())
	isTTY := term.IsTerminal(fd)

	if isTTY {
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, oldState)
		}

		initWindowSize(fd, client)
		go watchWindowSize(fd, client)

		fmt.Fprintf(os.Stderr, "[*] Connected to %s — Ctrl+C to exit\n", sessionID)
	} else {
		fmt.Fprintf(os.Stderr, "[*] Connected to %s (pipe mode)\n", sessionID)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		os.Exit(0)
	}()

	done := make(chan struct{}, 1)
	go func() {
		io.Copy(os.Stdout, channel)
		close(done)
	}()

	stdinDone := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if _, werr := channel.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("[connect] stdin: %v", err)
				}
				break
			}
		}
		close(stdinDone)
	}()

	select {
	case <-done:
	case <-stdinDone:
		if isTTY {
			channel.CloseWrite()
		}
		<-done
	}
	time.Sleep(200 * time.Millisecond)
}

func initWindowSize(fd int, client *ssh.Client) {
	w, h, err := term.GetSize(fd)
	if err != nil || w == 0 || h == 0 {
		return
	}
	payload := make([]byte, 16)
	binary.BigEndian.PutUint32(payload[0:4], uint32(w))
	binary.BigEndian.PutUint32(payload[4:8], uint32(h))
	binary.BigEndian.PutUint32(payload[8:12], 0)
	binary.BigEndian.PutUint32(payload[12:16], 0)
	_, _, _ = client.SendRequest("window-change", false, payload)
}

func watchWindowSize(fd int, client *ssh.Client) {
	sigCh := make(chan os.Signal, 1)
	notifyWinch(sigCh)
	for range sigCh {
		w, h, err := term.GetSize(fd)
		if err != nil {
			continue
		}
		payload := make([]byte, 16)
		binary.BigEndian.PutUint32(payload[0:4], uint32(w))
		binary.BigEndian.PutUint32(payload[4:8], uint32(h))
		binary.BigEndian.PutUint32(payload[8:12], 0)
		binary.BigEndian.PutUint32(payload[12:16], 0)
		_, _, _ = client.SendRequest("window-change", false, payload)
	}
}
