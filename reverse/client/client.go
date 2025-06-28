// client.go
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
	KeepAliveInterval     int    `json:"keepAliveInterval"` // milliseconds
	ConnectionWriteTimeout int    `json:"connectionWriteTimeout"` // milliseconds
	MaxStreamWindowSize   uint32 `json:"maxStreamWindowSize"`
}

// Config holds client configuration
type Config struct {
	TunnelServerAddr string     `json:"tunnelServerAddr"`
	LocalListenAddr  string     `json:"localListenAddr"`
	Yamux            YamuxConfig `json:"yamux"`
	PublicKeyPath    string     `json:"publicKeyPath"`
	SecretToken      string     `json:"secretToken"`
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
	// Load config
	file, err := os.Open("client_config.json")
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

	// Load server public key (path from config)
	publicKey, err := loadPublicKey(cfg.PublicKeyPath)
	if err != nil {
		log.Fatalf("Failed to load public key: %v", err)
	}

	log.Println("Connecting to tunnel server at", cfg.TunnelServerAddr)
	// Connect to tunnel server
	tunnelConn, err := net.Dial("tcp", cfg.TunnelServerAddr)
	if err != nil {
		log.Fatalf("Failed to connect to tunnel server: %v", err)
	}
	log.Println("Tunnel TCP connection established")

	// --- AUTHENTICATION HANDSHAKE ---
	token := []byte(cfg.SecretToken)
	encToken, err := rsa.EncryptPKCS1v15(rand.Reader, publicKey, token)
	if err != nil {
		log.Fatalf("Failed to encrypt token: %v", err)
	}
	if _, err := tunnelConn.Write(encToken); err != nil {
		log.Fatalf("Failed to send encrypted token: %v", err)
	}
	log.Println("Sent encrypted token to server for authentication")
	// --- END AUTHENTICATION HANDSHAKE ---

	// Create yamux client session
	session, err := yamux.Client(tunnelConn, yamuxConf)
	if err != nil {
		log.Fatalf("Failed to create yamux session: %v", err)
	}
	log.Println("Yamux session established with server")

	// Accept yamux streams in a loop
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Printf("Failed to accept yamux stream: %v", err)
			return
		}
		log.Println("Accepted new yamux stream from server")
		go func(stream net.Conn) {
			defer stream.Close()
			// Connect to local xray-core (or any local service)
			localConn, err := net.Dial("tcp", cfg.LocalListenAddr)
			if err != nil {
				log.Printf("Failed to connect to local service: %v", err)
				return
			}
			log.Println("Connected to local service for new stream")
			defer localConn.Close()
			// Forward data between yamux stream and local service
			go io.Copy(localConn, stream)
			io.Copy(stream, localConn)
		}(stream)
	}
}