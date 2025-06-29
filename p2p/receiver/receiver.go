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

// ReceiverConfig holds the configuration for the receiver server
type ReceiverConfig struct {
	ListenAddr  string `json:"listenAddr"`
	ForwardAddr string `json:"forwardAddr"`
	CertFile    string `json:"certFile"`
	KeyFile     string `json:"keyFile"`
}

func loadReceiverConfig(path string) (*ReceiverConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg ReceiverConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	configPath := flag.String("config", "receiver_config.json", "Path to config file")
	flag.Parse()

	cfg, err := loadReceiverConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		log.Fatalf("Failed to load cert/key: %v", err)
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	listener, err := tls.Listen("tcp", cfg.ListenAddr, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", cfg.ListenAddr, err)
	}
	defer listener.Close()

	log.Printf("Receiver started. Listening on %s, forwarding to %s", cfg.ListenAddr, cfg.ForwardAddr)

	for {
		entryConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept entry connection: %v", err)
			continue
		}
		go handleReceiverConnection(entryConn, cfg.ForwardAddr)
	}
}

func handleReceiverConnection(entryConn net.Conn, forwardAddr string) {
	defer entryConn.Close()

	targetConn, err := net.Dial("tcp", forwardAddr)
	if err != nil {
		log.Printf("Failed to connect to forward address: %v", err)
		return
	}
	defer targetConn.Close()

	done := make(chan struct{})
	go func() {
		io.Copy(targetConn, entryConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(entryConn, targetConn)
		done <- struct{}{}
	}()
	<-done
}
