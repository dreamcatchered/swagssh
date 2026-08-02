//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"golang.org/x/crypto/ssh"
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
		log.Fatalf("Session not found: %v", err)
	}
	defer channel.Close()

	go ssh.DiscardRequests(requests)

	fmt.Fprintf(os.Stderr, "[*] Connected to %s\n", sessionID)

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
				break
			}
		}
		close(stdinDone)
	}()

	select {
	case <-done:
	case <-stdinDone:
		<-done
	}
}
