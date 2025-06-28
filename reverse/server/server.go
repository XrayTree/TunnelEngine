// server.go
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/hashicorp/yamux"
)

// YamuxConfig holds yamux configuration
type YamuxConfig struct {
	AcceptBacklog         int    `json:"acceptBacklog"`
	EnableKeepAlive       bool   `json:"enableKeepAlive"`
	KeepAliveInterval     int    `json:"keepAliveInterval"`         // milliseconds
	ConnectionWriteTimeout int    `json:"connectionWriteTimeout"`    // milliseconds
	MaxStreamWindowSize   uint32 `json:"maxStreamWindowSize"`
}

// Config holds server configuration
type Config struct {
	TunnelListenAddr string     `json:"tunnelListenAddr"`
	UserListenAddr   string     `json:"userListenAddr"`
	Yamux            YamuxConfig `json:"yamux"`
	PrivateKeyPath   string     `json:"privateKeyPath"`
	SecretToken      string     `json:"secretToken"`
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
	// Load config
	file, err := os.Open("server_config.json")
	if err != nil {
		log.Fatalf("Failed to open config: %v", err)
	}
	defer file.Close()
	var cfg Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		log.Fatalf("Failed to decode config: %v", err)
	}
	// Build yamux.Config from cfg.Yamux
	yamuxConf := yamux.DefaultConfig()
	yamuxConf.AcceptBacklog = cfg.Yamux.AcceptBacklog
	yamuxConf.EnableKeepAlive = cfg.Yamux.EnableKeepAlive
	yamuxConf.KeepAliveInterval = time.Duration(cfg.Yamux.KeepAliveInterval) * time.Millisecond
	yamuxConf.ConnectionWriteTimeout = time.Duration(cfg.Yamux.ConnectionWriteTimeout) * time.Millisecond
	if cfg.Yamux.MaxStreamWindowSize > 0 {
		yamuxConf.MaxStreamWindowSize = cfg.Yamux.MaxStreamWindowSize
	}

	// Load server private key (path from config)
	privateKey, err := loadPrivateKey(cfg.PrivateKeyPath)
	if err != nil {
		log.Fatalf("Failed to load private key: %v", err)
	}

	// Listen for tunnel client
	tunnelListener, err := net.Listen("tcp", cfg.TunnelListenAddr)
	if err != nil {
		log.Fatalf("Failed to listen for tunnel client: %v", err)
	}
	log.Println("Waiting for tunnel client...")
	tunnelConn, err := tunnelListener.Accept()
	if err != nil {
		log.Fatalf("Failed to accept tunnel client: %v", err)
	}
	log.Println("Tunnel client connected")

	// --- AUTHENTICATION HANDSHAKE ---
	encToken := make([]byte, 256) // 256 bytes for 2048-bit key
	if _, err := io.ReadFull(tunnelConn, encToken); err != nil {
		log.Fatalf("Failed to read encrypted token: %v", err)
	}
	token, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, encToken)
	if err != nil {
		log.Fatalf("Failed to decrypt token: %v", err)
	}
	if string(token) != cfg.SecretToken {
		log.Fatalf("Invalid token from client")
	}
	log.Println("Client authenticated successfully")
	// --- END AUTHENTICATION HANDSHAKE ---

	// Create yamux server session
	session, err := yamux.Server(tunnelConn, yamuxConf)
	if err != nil {
		log.Fatalf("Failed to create yamux session: %v", err)
	}

	// Listen for external clients on userListenAddr
	userListener, err := net.Listen("tcp", cfg.UserListenAddr)
	if err != nil {
		log.Fatalf("Failed to listen for user connections: %v", err)
	}
	log.Printf("Listening for external clients on %s", cfg.UserListenAddr)

	for {
		userConn, err := userListener.Accept()
		if err != nil {
			log.Printf("Failed to accept user connection: %v", err)
			continue
		}
		log.Println("Accepted connection from external client")
		go func(userConn net.Conn) {
			defer userConn.Close()
			// Open a new yamux stream to the client
			stream, err := session.OpenStream()
			if err != nil {
				log.Printf("Failed to open yamux stream: %v", err)
				return
			}
			defer stream.Close()
			// Forward data between userConn and yamux stream
			go io.Copy(stream, userConn)
			io.Copy(userConn, stream)
		}(userConn)
	}
}