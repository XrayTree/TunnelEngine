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

// BufferPool is a pool of reusable buffers
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024) // 32KB buffer size
	},
}

// copyBuffer copies from src to dst using a buffer from the pool
func copyBuffer(dst io.Writer, src io.Reader) (written int64, err error) {
	buf := bufferPool.Get().([]byte)
	defer bufferPool.Put(buf)
	return io.CopyBuffer(dst, src, buf)
}

// YamuxConfig holds yamux configuration
type YamuxConfig struct {
	AcceptBacklog          int    `json:"acceptBacklog"`
	EnableKeepAlive        bool   `json:"enableKeepAlive"`
	KeepAliveInterval      int    `json:"keepAliveInterval"`      // milliseconds
	ConnectionWriteTimeout int    `json:"connectionWriteTimeout"` // milliseconds
	MaxStreamWindowSize    uint32 `json:"maxStreamWindowSize"`
}

// Config holds server configuration
type Config struct {
	TunnelListenAddr string      `json:"tunnelListenAddr"`
	UserListenAddr   []string    `json:"userListenAddr"`
	Yamux            YamuxConfig `json:"yamux"`
	PrivateKeyPath   string      `json:"privateKeyPath"`
	SecretToken      string      `json:"secretToken"`
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
						handleStream(userConn, stream)
					}(userConn)
				}
			}(userListener)
		}
		// Wait for stopChan to be closed (session or OpenStream failure)
		<-stopChan
		for _, l := range listeners {
			l.Close()
		}
		session.Close()
		tunnelConn.Close()
		log.Println("Connection lost, retrying in 3 seconds...")
		time.Sleep(3 * time.Second)
	}
}

// handleStream manages a single stream connection
func handleStream(userConn net.Conn, stream net.Conn) {
	defer userConn.Close()
	defer stream.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// Copy from user to stream
	go func() {
		defer wg.Done()
		copyBuffer(stream, userConn)
	}()

	// Copy from stream to user
	go func() {
		defer wg.Done()
		copyBuffer(userConn, stream)
	}()

	wg.Wait()
}
