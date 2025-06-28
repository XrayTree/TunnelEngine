# TunnelEngine Simple Port Forwarder

This is a simple port forwarder written in Go. It allows you to forward traffic from a local port to a remote destination.

## Features

- Forward traffic from one port to another
- Supports:
    - SAMEPORT => SAMEPORT (e.g., 9090 => 9090)
    - DIFFERENTPORT => DIFFERENTPORT (e.g., 9090 => 10002)

## Usage

1. **Create a `config.json` file** in the same directory as `simple.go` with the following structure:

```json
{
    "localAddr": "127.0.0.1:9090",
    "remoteAddr": "somedestination:10002"
}
```

        - `localAddr`: The local address and port to listen on (can be `127.0.0.1:PORT`).
        - `remoteAddr`: The remote address and port to forward to (can be a domain or IP).

2. **Run the forwarder:**

```sh
go run simple.go
```

Traffic sent to `localAddr` will be forwarded to `remoteAddr`.
