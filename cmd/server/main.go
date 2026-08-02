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
	operator  chan operatorHandoff
	done      chan struct{}
	closed    atomic.Bool
}

type operatorHandoff struct {
	channel ssh.Channel
	done    chan struct{}
}

type Server struct {
	config        *ssh.ServerConfig
	sessions      sync.Map
	port          string
	hostKey       string
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
		var activeCount int
		s.sessions.Range(func(key, value interface{}) bool {
			sess := value.(*Session)
			if now.Sub(sess.CreatedAt) > sessionTTL {
				log.Printf("[cleanup] TTL expired: %s", sess.ID)
				sess.Close()
				s.sessions.Delete(key)
			} else if !sess.closed.Load() {
				activeCount++
			}
			return true
		})
		log.Printf("[cleanup] Active sessions: %d", activeCount)
	}
}

func (s *Server) sessionCount() int {
	count := 0
	s.sessions.Range(func(_, value interface{}) bool {
		sess := value.(*Session)
		if !sess.closed.Load() {
			count++
		}
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

func bannerBox(sessionID string) string {
	line := strings.Repeat("═", 50)
	idLine := fmt.Sprintf("Session ID: %s", sessionID)
	connLine := fmt.Sprintf("swagssh connect %s", sessionID)

	b := fmt.Sprintf("\033[1;36m╔%s╗\033[0m\n", line)
	b += fmt.Sprintf("\033[1;36m║\033[0m  \033[1;32mswagSSH Session Ready\033[0m%s\033[1;36m║\033[0m\n", spaces(48-24))
	b += fmt.Sprintf("\033[1;36m║\033[0m  %s%s\033[1;36m║\033[0m\n", "", spaces(48))
	b += fmt.Sprintf("\033[1;36m║\033[0m  \033[1;33m%s\033[0m%s\033[1;36m║\033[0m\n", idLine, spaces(48-2-len(idLine)))
	b += fmt.Sprintf("\033[1;36m║\033[0m  %s%s\033[1;36m║\033[0m\n", "", spaces(48))
	b += fmt.Sprintf("\033[1;36m║\033[0m  \033[1m%s\033[0m%s\033[1;36m║\033[0m\n", connLine, spaces(48-2-len(connLine)))
	b += fmt.Sprintf("\033[1;36m║\033[0m  %s%s\033[1;36m║\033[0m\n", "", spaces(48))
	b += fmt.Sprintf("\033[1;36m╚%s╝\033[0m\n", line)
	return b
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	s := make([]byte, n)
	for i := range s {
		s[i] = ' '
	}
	return string(s)
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
			operator:  make(chan operatorHandoff, 1),
			done:      make(chan struct{}),
		}

		s.sessions.Store(sessionID, sess)
		s.totalSessions.Add(1)

		go func() {
			sess.ShareConn.Wait()
			sess.Close()
		}()

		log.Printf("[+] Session created: %s from %s (total: %d)", sessionID, sshConn.RemoteAddr(), s.sessionCount())

		channel.Write([]byte(bannerBox(sessionID)))

		for {
			select {
			case handoff := <-sess.operator:
				log.Printf("[bridge] Operator connected to %s", sessionID)
				s.bridge(sess, handoff)
				log.Printf("[bridge] Operator disconnected from %s", sessionID)
			case <-sess.done:
				log.Printf("[-] Session ended: %s", sessionID)
				s.sessions.Delete(sessionID)
				return
			case <-time.After(sessionTTL):
				log.Printf("[cleanup] Session %s expired (1h TTL)", sessionID)
				sess.Close()
				s.sessions.Delete(sessionID)
				return
			}
		}
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

		handoff := operatorHandoff{channel: channel, done: make(chan struct{})}

		select {
		case sess.operator <- handoff:
			log.Printf("[bridge] Operator channel handed off for %s", sessionID)
			<-handoff.done
			return
		case <-sess.done:
			channel.Write([]byte("\033[1;31mSession ended\033[0m\n"))
			channel.Close()
			return
		case <-time.After(10 * time.Second):
			channel.Write([]byte("\033[1;31mConnection timed out\033[0m\n"))
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
			sess.mu.Lock()
			if sess.Channel != nil {
				_, err := sess.Channel.SendRequest("window-change", req.WantReply, req.Payload)
				if req.WantReply {
					req.Reply(err == nil, nil)
				}
			} else if req.WantReply {
				req.Reply(false, nil)
			}
			sess.mu.Unlock()
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (s *Server) bridge(sess *Session, handoff operatorHandoff) {
	opChannel := handoff.channel
	defer close(handoff.done)
	defer opChannel.Close()

	opDone := make(chan struct{}, 1)
	shareDone := make(chan struct{}, 1)

	go func() {
		n, err := io.Copy(sess.Channel, opChannel)
		log.Printf("[bridge] op->share %d bytes, err=%v", n, err)
		close(opDone)
	}()

	go func() {
		n, err := io.Copy(opChannel, sess.Channel)
		log.Printf("[bridge] share->op %d bytes, err=%v", n, err)
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
	hostKey := flag.String("host-key", "", "Path to Ed25519 host key file")

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
