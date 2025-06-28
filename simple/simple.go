package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
)

// Config holds the configuration loaded from config.json
type Config struct {
	LocalAddr  string `json:"localAddr"`
	RemoteAddr string `json:"remoteAddr"`
}

func loadConfig(path string) (*Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	cfg, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	localAddr := cfg.LocalAddr
	remoteAddr := cfg.RemoteAddr

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", localAddr, err)
	}
	defer listener.Close()

	log.Printf("Port Forwarder started. Listening on %s", localAddr)
	log.Printf("Forwarding traffic to %s", remoteAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept client connection: %v", err)
			continue
		}

		log.Printf("Accepted connection from %s", clientConn.RemoteAddr())

		go handleConnection(clientConn, remoteAddr)
	}
}

func handleConnection(clientConn net.Conn, remoteAddr string) {

	defer clientConn.Close()

	remoteConn, err := net.Dial("tcp", remoteAddr)
	if err != nil {
		log.Printf("Failed to connect to remote host %s: %v", remoteAddr, err)
		return
	}

	defer remoteConn.Close()

	log.Printf("Forwarding from %s to %s", clientConn.RemoteAddr(), remoteConn.RemoteAddr())

	done := make(chan struct{})

	go func() {

		_, err := io.Copy(remoteConn, clientConn)
		if err != nil {

			log.Printf("Error copying from client to remote: %v", err)
		}
		done <- struct{}{}
	}()

	go func() {
		_, err := io.Copy(clientConn, remoteConn)
		if err != nil {
			log.Printf("Error copying from remote to client: %v", err)
		}
		done <- struct{}{}
	}()

	<-done
	log.Printf("Closing connection for %s", clientConn.RemoteAddr())
}
