// server.go
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// YamuxConfig holds yamux configuration
type YamuxConfig struct {
	AcceptBacklog          int    `json:"acceptBacklog"`
	EnableKeepAlive        bool   `json:"enableKeepAlive"`
	KeepAliveInterval      int    `json:"keepAliveInterval"`      // milliseconds
	ConnectionWriteTimeout int    `json:"connectionWriteTimeout"` // milliseconds
	MaxStreamWindowSize    uint32 `json:"maxStreamWindowSize"`
	MaxConcurrentConnections int  `json:"maxConcurrentConnections"`
}

// Config holds server configuration
type Config struct {
   TunnelListenAddr string      `json:"tunnelListenAddr"`
   UserListenAddr   []string    `json:"userListenAddr"`
   Yamux            YamuxConfig `json:"yamux"`
   PrivateKeyPath   string      `json:"privateKeyPath"`
   SecretToken      string      `json:"secretToken"`
   UseMux           bool        `json:"useMux"`
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block containing private key")
	}
	if block.Type == "RSA PRIVATE KEY" {
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	} else if block.Type == "PRIVATE KEY" {
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		} else {
			return nil, errors.New("not an RSA private key in PKCS#8 format")
		}
	} else {
		return nil, errors.New("unsupported private key type: " + block.Type)
	}
}

func main() {
	configPath := flag.String("config", "server_config.json", "Path to config file")
	flag.Parse()
	for {
		// Load config
		file, err := os.Open(*configPath)
		if err != nil {
			log.Fatalf("Failed to open config: %v", err)
		}
		var cfg Config
		if err := json.NewDecoder(file).Decode(&cfg); err != nil {
			file.Close()
			log.Fatalf("Failed to decode config: %v", err)
		}
		file.Close()
		// Build yamux.Config from cfg.Yamux
		yamuxConf := yamux.DefaultConfig()
		yamuxConf.AcceptBacklog = cfg.Yamux.AcceptBacklog
		yamuxConf.EnableKeepAlive = cfg.Yamux.EnableKeepAlive
		yamuxConf.KeepAliveInterval = time.Duration(cfg.Yamux.KeepAliveInterval) * time.Millisecond
		yamuxConf.ConnectionWriteTimeout = time.Duration(cfg.Yamux.ConnectionWriteTimeout) * time.Millisecond
		if cfg.Yamux.MaxStreamWindowSize > 0 {
			yamuxConf.MaxStreamWindowSize = cfg.Yamux.MaxStreamWindowSize
		}
		maxConcurrentStreams := cfg.Yamux.MaxConcurrentConnections

		// Load server private key (path from config)
		privateKey, err := loadPrivateKey(cfg.PrivateKeyPath)
		if err != nil {
			log.Printf("Failed to load private key: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		// Listen for tunnel client
		tunnelListener, err := net.Listen("tcp", cfg.TunnelListenAddr)
		if err != nil {
			log.Printf("Failed to listen for tunnel client: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		log.Println("Waiting for tunnel client...")
		tunnelConn, err := tunnelListener.Accept()
		if err != nil {
			log.Printf("Failed to accept tunnel client: %v", err)
			tunnelListener.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		log.Println("Tunnel client connected")
		tunnelListener.Close()

		// --- AUTHENTICATION HANDSHAKE ---
		encToken := make([]byte, 256) // 256 bytes for 2048-bit key
		if _, err := io.ReadFull(tunnelConn, encToken); err != nil {
			log.Printf("Failed to read encrypted token: %v", err)
			tunnelConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		token, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encToken)
		if err != nil {
			log.Printf("Failed to decrypt token: %v", err)
			tunnelConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		if string(token) != cfg.SecretToken {
			log.Printf("Invalid token from client")
			tunnelConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		log.Println("Client authenticated successfully")
		// --- END AUTHENTICATION HANDSHAKE ---

		if cfg.UseMux {
			// Create yamux server session
			session, err := yamux.Server(tunnelConn, yamuxConf)
			if err != nil {
				log.Printf("Failed to create yamux session: %v", err)
				tunnelConn.Close()
				time.Sleep(3 * time.Second)
				continue
			}

			// Listen for external clients on userListenAddr
			var listeners []net.Listener
			for _, addr := range cfg.UserListenAddr {
				l, err := net.Listen("tcp", addr)
				if err != nil {
					log.Printf("Failed to listen on %s: %v", addr, err)
					continue
				}
				log.Printf("Listening for external clients on %s", addr)
				listeners = append(listeners, l)
			}
			if len(listeners) == 0 {
				log.Printf("No user listeners available, retrying in 3 seconds...")
				session.Close()
				tunnelConn.Close()
				time.Sleep(3 * time.Second)
				continue
			}

			// Accept connections on all listeners
			stopChan := make(chan struct{})
			var streamCountMu sync.Mutex
			streamCount := 0
			for _, userListener := range listeners {
				go func(userListener net.Listener) {
					for {
						select {
						case <-stopChan:
							userListener.Close()
							return
						default:
						}
						userConn, err := userListener.Accept()
						if err != nil {
							select {
							case <-stopChan:
								return
							default:
								log.Printf("Failed to accept user connection: %v", err)
								break
							}
						}
						log.Println("Accepted connection from external client")
						go func(userConn net.Conn) {
							defer userConn.Close()
							// Limit concurrent yamux streams
							streamCountMu.Lock()
							if maxConcurrentStreams > 0 && streamCount >= maxConcurrentStreams {
								streamCountMu.Unlock()
								log.Printf("Max concurrent yamux streams reached (%d), rejecting connection", maxConcurrentStreams)
								return
							}
							streamCount++
							streamCountMu.Unlock()
							defer func() {
								streamCountMu.Lock()
								streamCount--
								streamCountMu.Unlock()
							}()
							// Open a new yamux stream to the client
							stream, err := session.OpenStream()
							if err != nil {
								log.Printf("Failed to open yamux stream: %v", err)
								// If session is closed, signal all listeners to stop
								select {
								case <-stopChan:
									// already closed
								default:
									close(stopChan)
								}
								return
							}
							defer stream.Close()
							// Forward data between userConn and yamux stream
							go io.Copy(stream, userConn)
							io.Copy(userConn, stream)
						}(userConn)
					}
				}(userListener)
			}
	   // (Removed unreachable/duplicate cleanup code)
		} else {
			// No yamux: handle tunnel as a single connection
			for _, addr := range cfg.UserListenAddr {
				userListener, err := net.Listen("tcp", addr)
				if err != nil {
					log.Printf("Failed to listen on %s: %v", addr, err)
					continue
				}
				log.Printf("Listening for external clients on %s (no mux)", addr)
				go func(userListener net.Listener, tunnelConn net.Conn) {
					for {
						userConn, err := userListener.Accept()
						if err != nil {
							log.Printf("Failed to accept user connection: %v", err)
							break
						}
						log.Println("Accepted connection from external client (no mux)")
						go func(userConn net.Conn, tunnelConn net.Conn) {
							defer userConn.Close()
							// Forward data directly between userConn and tunnelConn
							go io.Copy(tunnelConn, userConn)
							io.Copy(userConn, tunnelConn)
						}(userConn, tunnelConn)
					}
				}(userListener, tunnelConn)
			}
			// Wait for tunnelConn to close
			done := make(chan struct{})
			go func() {
				buf := make([]byte, 1)
				_, err := tunnelConn.Read(buf)
				if err != nil {
					close(done)
				}
			}()
			<-done
			tunnelConn.Close()
			log.Println("Tunnel connection closed, retrying in 3 seconds...")
			time.Sleep(3 * time.Second)
		}
	   // (Removed unreachable/duplicate cleanup code)
	}
}
