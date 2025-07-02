# TunnelEngine

TunnelEngine is a flexible tunneling engine written in Go, supporting fast IP forwarding, encrypted P2P tunnels, and multiplexed reverse tunneling. It helps you securely expose local services, forward ports, and create encrypted tunnels between servers, even across NAT or firewalls.

## Features

- **Reverse Tunnel:** Multiplexed, encrypted reverse proxy using Yamux and RSA authentication.
- **P2P Tunnel:** Encrypted peer-to-peer tunnels using TLS (in development).
- **Simple Forwarder:** Lightweight TCP port forwarder.
- **Open Source:** All code is available for review and customization.

## Quick Start

### 1. Download Binaries

Pre-built binaries are available on the [GitHub Releases page](https://github.com/XrayTree/TunnelEngine/releases).  
All binaries (`server-linux-amd64`, `client-linux-amd64`, `receiver-linux-amd64`, `simple-linux-amd64`) now support the `-config` flag.

> **Note:** Binaries are tested on Ubuntu/Debian. Other OS support and additional features are coming soon.

### 2. Generate RSA Key Pair

On the server (public host), generate an RSA key pair for authentication:

```sh
openssl genpkey -algorithm RSA -out server_private.pem -pkeyopt rsa_keygen_bits:2048
openssl rsa -pubout -in server_private.pem -out server_public.pem
```

- Place `server_private.pem` on the server.
- Place `server_public.pem` on the client.

### 3. Create Configuration Files

#### Server (`reverse/server/server_config.json`)

See [`reverse/README.md`](reverse/README.md) for full details. Example:

```json
{
  "tunnelListenAddr": ":9090",
  "userListenAddr": [":10004", ":10005"], // more ports if needed.
  "yamux": {
    "acceptBacklog": 256,
    "enableKeepAlive": true,
    "keepAliveInterval": 15000,
    "connectionWriteTimeout": 30000,
    "maxStreamWindowSize": 4194304
  },
  "privateKeyPath": "/root/server_private.pem",
  "secretToken": "somethingsecret"
}
```

#### Client (`reverse/client/client_config.json`)

See [`reverse/README.md`](reverse/README.md) for full details. Example:

```json
{
  "tunnelServerAddr": "destIp:9090",
  "localListenAddr": [":10004", ":10005"], // more ports if needed.
  "yamux": {
    "acceptBacklog": 256,
    "enableKeepAlive": true,
    "keepAliveInterval": 15000,
    "connectionWriteTimeout": 30000,
    "maxStreamWindowSize": 4194304
  },
  "publicKeyPath": "/root/server_public.pem",
  "secretToken": "somethingsecret"
}
```

- The `secretToken` must match on both server and client.

### 4. Run the Binaries

On the **server** (behind firewall/public host):

```sh
./server-linux-amd64 -config server_config.json
```

On the **client** (freedom/private host):

```sh
./client-linux-amd64 -config client_config.json
```

- The server will listen for tunnel connections and external user connections.
- The client will connect to the server and expose your local service.


## Building from Source

All code is open source. You can build any component manually:

```sh
cd reverse/server
go build -o server
cd ../client
go build -o client
```

See the README in each subfolder for details and advanced usage.

## Project Status & Roadmap

TunnelEngine is in its early stages. Documentation and support for other OSs are limited. All binaries now support the `-config` flag. P2P and simple forwarder binaries are under development.

- **Tested:** Ubuntu/Debian (x86_64)
- **Planned:** More documentation, more OS builds, and expanded features.

## Contributing

Contributions and feedback are welcome! Please open issues or pull requests.

---

For more details, see:
- [reverse/README.md](reverse/README.md)
- [p2p/README.md](p2p/README.md)
- [simple/README.md](simple/README.md)
