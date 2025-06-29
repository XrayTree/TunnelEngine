package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net"
	"os"
)

// EntryConfig holds the configuration for the entry client
type EntryConfig struct {
	LocalAddr    string `json:"localAddr"`
	ReceiverAddr string `json:"receiverAddr"`
}

func loadEntryConfig(path string) (*EntryConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg EntryConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	configPath := flag.String("config", "entry_config.json", "Path to config file")
	flag.Parse()

	cfg, err := loadEntryConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	listener, err := net.Listen("tcp", cfg.LocalAddr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", cfg.LocalAddr, err)
	}
	defer listener.Close()

	log.Printf("Entry started. Listening on %s, forwarding to receiver at %s", cfg.LocalAddr, cfg.ReceiverAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept client connection: %v", err)
			continue
		}
		go handleEntryConnection(clientConn, cfg.ReceiverAddr)
	}
}

func handleEntryConnection(clientConn net.Conn, receiverAddr string) {
	defer clientConn.Close()

	tlsConn, err := tls.Dial("tcp", receiverAddr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		log.Printf("Failed to connect to receiver: %v", err)
		return
	}
	defer tlsConn.Close()

	done := make(chan struct{})
	go func() {
		io.Copy(tlsConn, clientConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(clientConn, tlsConn)
		done <- struct{}{}
	}()
	<-done
}
