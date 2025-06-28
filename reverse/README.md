# TunnelEngine Reverse Tunnel Guide

This guide explains how to set up and run the reverse tunnel using the scripts in this directory. The reverse tunnel allows you to expose a local service (e.g., a web server running on your private network) to a remote server, even if the local machine is behind NAT or a firewall.

---

## 1. Overview

The reverse tunnel consists of two components:

- **Server** (`server/server.go`): Runs on the public server. Listens for tunnel connections from the client and for external user connections.
- **Client** (`client/client.go`): Runs on the private/local machine. Connects to the server and exposes a local service to the outside world via the tunnel.

---

## 2. Configuration Files

You must create two JSON configuration files before running the programs.

### a. `server_config.json`

**Location:** `reverse/server/server_config.json`

**Example:**
```json
{
  "tunnelListenAddr": "0.0.0.0:9000",
  "userListenAddr": "0.0.0.0:8080",
  "yamux": {
    "acceptBacklog": 128,
    "enableKeepAlive": true,
    "keepAliveInterval": 10000,
    "connectionWriteTimeout": 10000,
    "maxStreamWindowSize": 16777216
  },
  "privateKeyPath": "server_private.pem",
  "secretToken": "my_secret_token"
}
```

- `tunnelListenAddr`: Address and port for the client to connect to (e.g., `"0.0.0.0:9000"`).
- `userListenAddr`: Address and port for external users to connect to (e.g., `"0.0.0.0:8080"`).
- `yamux`: Yamux multiplexer settings (use the example values or adjust as needed).
- `privateKeyPath`: Path to the server's private key in PEM format.
- `secretToken`: Shared secret for authentication (must match the client).

---

### b. `client_config.json`

**Location:** `reverse/client/client_config.json`

**Example:**
```json
{
  "tunnelServerAddr": "your.server.ip:9000",
  "localListenAddr": "127.0.0.1:10000",
  "yamux": {
    "acceptBacklog": 128,
    "enableKeepAlive": true,
    "keepAliveInterval": 10000,
    "connectionWriteTimeout": 10000,
    "maxStreamWindowSize": 16777216
  },
  "publicKeyPath": "server_public.pem",
  "secretToken": "my_secret_token"
}
```

- `tunnelServerAddr`: Address and port of the server's tunnel listener (e.g., `"your.server.ip:9000"`).
- `localListenAddr`: Local address and port of the service to expose (e.g., `"127.0.0.1:10000"`).
- `yamux`: Yamux multiplexer settings (should match the server).
- `publicKeyPath`: Path to the server's RSA public key in PEM format.
- `secretToken`: Shared secret for authentication (must match the server).

---

## 3. Generating RSA Keys

You need an RSA key pair for authentication.

On the server, generate the keys:

```sh
openssl genpkey -algorithm RSA -out server_private.pem -pkeyopt rsa_keygen_bits:2048
openssl rsa -pubout -in server_private.pem -out server_public.pem
```

- Place `server_private.pem` on the server (path as in `server_config.json`).
- Place `server_public.pem` on the client (path as in `client_config.json`).

---

## 4. Running the Programs

### a. On the Server (Public Host)

1. Place `server.go`, `server_config.json`, and `server_private.pem` in the `reverse/server/` directory.
2. Open a terminal in `reverse/server/`.
3. Run the server:

   ```sh
   go run server.go
   ```

   The server will wait for a tunnel connection from the client and listen for external user connections.

---

### b. On the Client (Private/Local Host)

1. Place `client.go`, `client_config.json`, and `server_public.pem` in the `reverse/client/` directory.
2. Open a terminal in `reverse/client/`.
3. Run the client:

   ```sh
   go run client.go
   ```

   The client will connect to the server and expose the local service specified in `localListenAddr`.

---

## 5. How It Works

- External users connect to the server's `userListenAddr` (e.g., `your.server.ip:8080`).
- The server forwards these connections through the tunnel to the client.
- The client connects the tunnel stream to the local service at `localListenAddr`.

---

## 6. Troubleshooting

- Ensure both JSON config files are present and correctly filled.
- Make sure the RSA key files exist and are valid.
- The `secretToken` must match on both client and server.
- Open the necessary ports in your firewall/security group.
- Check logs for errors if connections fail.

---

**You now have a working reverse tunnel!**