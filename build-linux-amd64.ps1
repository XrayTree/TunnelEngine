# PowerShell script to build all targets for Linux amd64

$ErrorActionPreference = 'Stop'

Write-Host "Building client-linux-amd64..."
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o client-linux-amd64 reverse\client\client.go

Write-Host "Building server-linux-amd64..."
go build -o server-linux-amd64 reverse\server\server.go

Write-Host "Building entry-linux-amd64..."
go build -o entry-linux-amd64 p2p\entry\entry.go

Write-Host "Building receiver-linux-amd64..."
go build -o receiver-linux-amd64 p2p\receiver\receiver.go

Write-Host "Building simple-linux-amd64..."
go build -o simple-linux-amd64 simple\simple.go

Write-Host "Builds complete."
