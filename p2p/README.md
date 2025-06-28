# TunnelEngine P2P Usage Guide

This guide explains how to set up and run the TunnelEngine P2P tunnel, including how to create the required JSON configuration files.

---

## 1. Overview

TunnelEngine consists of two components:

- **Entry**: Listens locally and forwards connections to the receiver over TLS.
- **Receiver**: Accepts TLS connections from the entry and forwards them to a target server.

---

## 2. Configuration Files

You must create two JSON configuration files before running the programs.

### a. `entry_config.json`

**Location:** `p2p/entry/entry_config.json`

**Example:**
```json
{
  "localAddr": "127.0.0.1:8080",
  "receiverAddr": "127.0.0.1:1010"
}
```

**Fields:**
- `localAddr`: The local address and port the entry will listen on (e.g., `"127.0.0.1:8080"`).
- `receiverAddr`: The address and port of the receiver (e.g., `"127.0.0.1:1010"`).

---

### b. `receiver_config.json`

**Location:** `p2p/receiver/receiver_config.json`

**Example:**
```json
{
  "listenAddr": "0.0.0.0:1010",
  "forwardAddr": "somedestionation:10002",
  "certFile": "keys/cert.pem",
  "keyFile": "keys/key.pem"
}
```

**Fields:**
- `listenAddr`: The address and port the receiver will listen on (e.g., `"0.0.0.0:1010"`).
- `forwardAddr`: The address and port to forward incoming connections to (e.g., `somedestionation:10002"`).
- `certFile`: Path to the TLS certificate file (e.g., `"keys/cert.pem"`).
- `keyFile`: Path to the TLS private key file (e.g., `"keys/key.pem"`).

> **Note:** You must generate or provide your own TLS certificate and key files.

---

## 3. Generating TLS Certificate and Key

You need a TLS certificate and private key for the receiver. You can generate a self-signed certificate using OpenSSL.

**On the destination server (where the receiver will run):**

1. Open a terminal and navigate to the `p2p/receiver/keys` directory (create it if it doesn't exist):

   ```
   mkdir -p p2p/receiver/keys
   cd p2p/receiver/keys
   ```

2. Generate a self-signed certificate and key:

   ```
   openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes -subj "/CN=TunnelEngineReceiver"
   ```

3. Ensure your `receiver_config.json` points to these files:

   ```json
   "certFile": "keys/cert.pem",
   "keyFile": "keys/key.pem"
   ```

---

## 4. Deployment and Running the Programs

- **Entry** should be placed and run on the entry server (the server where your client or local application is located).
- **Receiver** should be placed and run on the destination server (the server you want to forward traffic to).

### Steps:

1. **Start the Receiver (on the destination server):**

   Open a terminal in `p2p/receiver` and run:
   ```
   go run receiver.go
   ```

2. **Start the Entry (on the entry server):**

   Open another terminal in `p2p/entry` and run:
   ```
   go run entry.go
   ```

---

## 5. How It Works

- Connect your client application to the `localAddr` specified in `entry_config.json`.
- The entry forwards traffic securely to the receiver.
- The receiver forwards traffic to the `forwardAddr` destination.

---

## 6. Troubleshooting

- Ensure both JSON config files are present and correctly filled.
- Make sure the certificate and key files exist and are valid.
- Check that the ports you specify are open and not in use.
- Ensure the entry and receiver are running on the correct servers.

---

**That's it!** You now have a secure tunnel between your entry