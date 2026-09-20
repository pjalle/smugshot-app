#!/bin/bash
# Builds the Linux version from any machine with Go installed. Output: build/smugshot-linux-amd64 and
# build/smugshot-linux-arm64, plain executables (no C, so no libraries to install). With a version
# (./scripts/build-linux.sh 0.4.0) it also makes build/Smugshot-<version>-linux.tar.gz with both.
set -euo pipefail
cd "$(dirname "$0")/../windows"
go test ./...
GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o ../build/smugshot-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -ldflags "-s -w" -o ../build/smugshot-linux-arm64 .
ls -la ../build/smugshot-linux-*
if [[ -n "${1:-}" ]]; then
  rm -f "../build/Smugshot-$1-linux.tar.gz"
  (cd ../build && tar -czf "Smugshot-$1-linux.tar.gz" smugshot-linux-amd64 smugshot-linux-arm64)
  ls -la "../build/Smugshot-$1-linux.tar.gz"
fi
