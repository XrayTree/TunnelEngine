// client.go
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
	AcceptBacklog         int    `json:"acceptBacklog"`
	EnableKeepAlive       bool   `json:"enableKeepAlive"`
	KeepAliveInterval     int    `json:"keepAliveInterval"` // milliseconds
	ConnectionWriteTimeout int    `json:"connectionWriteTimeout"` // milliseconds
	MaxStreamWindowSize   uint32 `json:"maxStreamWindowSize"`
	MaxConcurrentConnections int  `json:"maxConcurrentConnections"`
}

// Config holds client configuration
type Config struct {
	TunnelServerAddr string     `json:"tunnelServerAddr"`
	LocalListenAddr  []string     `json:"localListenAddr"`
	Yamux            YamuxConfig `json:"yamux"`
	PublicKeyPath    string     `json:"publicKeyPath"`
	SecretToken      string     `json:"secretToken"`
	UseMux           bool       `json:"useMux"`
}

func loadPublicKey(path string) (*rsa.PublicKey, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(keyBytes)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("failed to decode PEM block containing public key")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return pub.(*rsa.PublicKey), nil
}

func main() {
	configPath := flag.String("config", "client_config.json", "Path to config file")
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

		// Load server public key (path from config)
		publicKey, err := loadPublicKey(cfg.PublicKeyPath)
		if err != nil {
			log.Printf("Failed to load public key: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		log.Println("Connecting to tunnel server at", cfg.TunnelServerAddr)
		// Connect to tunnel server
		tunnelConn, err := net.Dial("tcp", cfg.TunnelServerAddr)
		if err != nil {
			log.Printf("Failed to connect to tunnel server: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}
		log.Println("Tunnel TCP connection established")

		// --- AUTHENTICATION HANDSHAKE ---
		token := []byte(cfg.SecretToken)
		encToken, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, token)
		if err != nil {
			log.Printf("Failed to encrypt token: %v", err)
			tunnelConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		if _, err := tunnelConn.Write(encToken); err != nil {
			log.Printf("Failed to send encrypted token: %v", err)
			tunnelConn.Close()
			time.Sleep(3 * time.Second)
			continue
		}
		log.Println("Sent encrypted token to server for authentication")
		// --- END AUTHENTICATION HANDSHAKE ---

		if cfg.UseMux {
			// Create yamux client session
			session, err := yamux.Client(tunnelConn, yamuxConf)
			if err != nil {
				log.Printf("Failed to create yamux session: %v", err)
				tunnelConn.Close()
				time.Sleep(3 * time.Second)
				continue
			}
			log.Println("Yamux session established with server")

			// Accept yamux streams in a loop, with concurrency limit
			var localIdx int
			var streamCountMu sync.Mutex
			streamCount := 0
			for {
				streamCountMu.Lock()
				if maxConcurrentStreams > 0 && streamCount >= maxConcurrentStreams {
					streamCountMu.Unlock()
					time.Sleep(100 * time.Millisecond)
					continue
				}
				streamCount++
				streamCountMu.Unlock()

				stream, err := session.AcceptStream()
				if err != nil {
					streamCountMu.Lock()
					streamCount--
					streamCountMu.Unlock()
					log.Printf("Failed to accept yamux stream: %v", err)
					break
				}
				log.Println("Accepted new yamux stream from server")
				// Pick local address in round-robin fashion
				localAddr := cfg.LocalListenAddr[localIdx]
				localIdx = (localIdx + 1) % len(cfg.LocalListenAddr)
				go func(stream net.Conn, localAddr string) {
					defer stream.Close()
					defer func() {
						streamCountMu.Lock()
						streamCount--
						streamCountMu.Unlock()
					}()
					// Connect to local xray-core (or any local service)
					localConn, err := net.Dial("tcp", localAddr)
					if err != nil {
						log.Printf("Failed to connect to local service at %s: %v", localAddr, err)
						return
					}
					log.Printf("Connected to local service %s for new stream", localAddr)
					defer localConn.Close()
					// Forward data between yamux stream and local service
					go io.Copy(localConn, stream)
					io.Copy(stream, localConn)
				}(stream, localAddr)
			}
			session.Close()
			tunnelConn.Close()
			log.Println("Connection lost, retrying in 3 seconds...")
			time.Sleep(3 * time.Second)
		} else {
			// No yamux: handle tunnel as a single connection
			var localIdx int
			localAddr := cfg.LocalListenAddr[localIdx]
			localIdx = (localIdx + 1) % len(cfg.LocalListenAddr)
			localConn, err := net.Dial("tcp", localAddr)
			if err != nil {
				log.Printf("Failed to connect to local service at %s: %v", localAddr, err)
				tunnelConn.Close()
				time.Sleep(3 * time.Second)
				continue
			}
			log.Printf("Connected to local service %s for tunnel", localAddr)
			go io.Copy(localConn, tunnelConn)
			io.Copy(tunnelConn, localConn)
			localConn.Close()
			tunnelConn.Close()
			log.Println("Connection closed, retrying in 3 seconds...")
			time.Sleep(3 * time.Second)
		}
	}
}