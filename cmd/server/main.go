package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	sessionIDLength = 6
	sessionPrefix   = "swag-"
	sessionTTL      = 1 * time.Hour
	cleanupInterval = 5 * time.Minute
)

type Session struct {
	ID        string
	ShareConn *ssh.ServerConn
	Channel   ssh.Channel
	CreatedAt time.Time
	mu        sync.Mutex
	operator  chan ssh.Channel
	done      chan struct{}
	closed    atomic.Bool
}

type Server struct {
	config     *ssh.ServerConfig
	sessions   sync.Map
	port       string
	hostKey    string
	totalSessions atomic.Int64
}

func generateSessionID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return sessionPrefix + hex.EncodeToString(b)[:sessionIDLength]
}

func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	if path == "" {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate ed25519 key: %w", err)
		}
		signer, err := ssh.NewSignerFromKey(key)
		if err != nil {
			return nil, fmt.Errorf("create signer: %w", err)
		}
		return signer, nil
	}

	data, err := os.ReadFile(path)
	if err == nil {
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("parse host key: %w", err)
		}
		return signer, nil
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read host key: %w", err)
	}

	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	privBlock, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(privBlock), 0600); err != nil {
		return nil, fmt.Errorf("write host key: %w", err)
	}

	log.Printf("[+] Generated and saved Ed25519 host key to %s", path)

	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return nil, fmt.Errorf("create signer: %w", err)
	}
	return signer, nil
}

func NewServer(port, hostKeyPath string) (*Server, error) {
	signer, err := loadOrGenerateHostKey(hostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}

	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}

	config.AddHostKey(signer)

	s := &Server{
		config:  config,
		port:    port,
		hostKey: hostKeyPath,
	}

	go s.cleanupLoop()

	return s, nil
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.sessions.Range(func(key, value interface{}) bool {
			sess := value.(*Session)
			if now.Sub(sess.CreatedAt) > sessionTTL {
				sess.Close()
			}
			return true
		})
		log.Printf("[cleanup] Active sessions: %d", s.sessionCount())
	}
}

func (s *Server) sessionCount() int {
	count := 0
	s.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%s", s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	log.Printf("[*] swagSSH relay server listening on %s", addr)
	log.Printf("[*] Active sessions: %d", s.sessionCount())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[!] Accept error: %v", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(netConn net.Conn) {
	defer netConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		log.Printf("[!] SSH handshake failed from %s: %v", netConn.RemoteAddr(), err)
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	username := sshConn.User()

	switch {
	case strings.HasPrefix(username, sessionPrefix):
		s.handleConnect(sshConn, chans, username)
	default:
		s.handleShare(sshConn, chans)
	}
}

func (s *Server) handleShare(sshConn *ssh.ServerConn, chans <-chan ssh.NewChannel) {
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("[!] Accept channel error: %v", err)
			return
		}

		go ssh.DiscardRequests(requests)

		sessionID := generateSessionID()
		for {
			if _, exists := s.sessions.Load(sessionID); !exists {
				break
			}
			sessionID = generateSessionID()
		}

		sess := &Session{
			ID:        sessionID,
			ShareConn: sshConn,
			Channel:   channel,
			CreatedAt: time.Now(),
			operator:  make(chan ssh.Channel, 1),
			done:      make(chan struct{}),
		}

		s.sessions.Store(sessionID, sess)
		s.totalSessions.Add(1)

		log.Printf("[+] Session created: %s from %s (total: %d)", sessionID, sshConn.RemoteAddr(), s.sessionCount())

		sendMsg := fmt.Sprintf("\033[1;36m╔══════════════════════════════════════════╗\n\033[0m")
		sendMsg += fmt.Sprintf("\033[1;36m║\033[0m     \033[1;32mswagSSH Session Ready\033[0m             \033[1;36m║\033[0m\n")
		sendMsg += fmt.Sprintf("\033[1;36m║\033[0m                                          \033[1;36m║\033[0m\n")
		sendMsg += fmt.Sprintf("\033[1;36m║\033[0m  Session ID: \033[1;33m%s\033[0m                 \033[1;36m║\033[0m\n", sessionID)
		sendMsg += fmt.Sprintf("\033[1;36m║\033[0m                                          \033[1;36m║\033[0m\n")
		sendMsg += fmt.Sprintf("\033[1;36m║\033[0m  Connect: \033[1mswagssh connect %s\033[0m         \033[1;36m║\033[0m\n", sessionID)
		sendMsg += fmt.Sprintf("\033[1;36m║\033[0m                                          \033[1;36m║\033[0m\n")
		sendMsg += fmt.Sprintf("\033[1;36m╚══════════════════════════════════════════╝\033[0m\n")
		channel.Write([]byte(sendMsg))

		select {
		case opChannel := <-sess.operator:
			s.bridge(sess, opChannel)
		case <-sess.done:
			return
		case <-time.After(sessionTTL):
			sess.Close()
			return
		}
		return
	}
}

func (s *Server) handleConnect(sshConn *ssh.ServerConn, chans <-chan ssh.NewChannel, sessionID string) {
	val, ok := s.sessions.Load(sessionID)
	if !ok {
		log.Printf("[!] Session not found: %s", sessionID)
		return
	}

	sess := val.(*Session)
	if sess.closed.Load() {
		log.Printf("[!] Session already closed: %s", sessionID)
		return
	}

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Printf("[!] Accept operator channel error: %v", err)
			return
		}

		go s.handleOperatorRequests(sess, requests)

		log.Printf("[+] Operator connected to session %s from %s", sessionID, sshConn.RemoteAddr())

		select {
		case sess.operator <- channel:
		case <-sess.done:
			channel.Close()
			return
		case <-time.After(10 * time.Second):
			channel.Write([]byte("\033[1;31mSession connection timed out\033[0m\n"))
			channel.Close()
			return
		}

		return
	}
}

func (s *Server) handleOperatorRequests(sess *Session, requests <-chan *ssh.Request) {
	for req := range requests {
		switch req.Type {
		case "window-change":
			if sess.Channel != nil {
				sess.mu.Lock()
				ok, err := sess.Channel.SendRequest("window-change", req.WantReply, req.Payload)
				sess.mu.Unlock()
				if req.WantReply {
					if err != nil {
						req.Reply(false, nil)
					} else {
						req.Reply(ok, nil)
					}
				}
			} else if req.WantReply {
				req.Reply(false, nil)
			}
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (s *Server) bridge(sess *Session, opChannel ssh.Channel) {
	defer sess.Close()

	defer opChannel.Close()
	defer sess.Channel.Close()

	opDone := make(chan struct{}, 1)
	shareDone := make(chan struct{}, 1)

	go func() {
		io.Copy(sess.Channel, opChannel)
		close(opDone)
	}()

	go func() {
		io.Copy(opChannel, sess.Channel)
		close(shareDone)
	}()

	select {
	case <-opDone:
	case <-shareDone:
	case <-sess.done:
	}
}

func (s *Session) Close() {
	if s.closed.Swap(true) {
		return
	}
	close(s.done)
	if s.Channel != nil {
		s.Channel.Close()
	}
	if s.ShareConn != nil {
		s.ShareConn.Close()
	}
}

func main() {
	port := flag.String("port", "2222", "Port to listen on")
	hostKey := flag.String("host-key", "", "Path to host key file (Ed25519 encoded in OpenSSH format, generated if missing)")

	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[swagSSH] ")

	server, err := NewServer(*port, *hostKey)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
