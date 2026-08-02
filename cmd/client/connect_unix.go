//go:build !windows

package main

import (
	"encoding/binary"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	if !term.IsTerminal(fd) {
		log.Fatal("Interactive terminal required")
	}

	oldState, err := term.MakeRaw(fd)
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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		os.Exit(0)
	}()

	errCh := make(chan error, 2)
	go func() { _, e := io.Copy(channel, os.Stdin); errCh <- e }()
	go func() { _, e := io.Copy(os.Stdout, channel); errCh <- e }()
	<-errCh
}
