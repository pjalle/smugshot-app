#!/bin/bash
# Builds the Windows version from any machine with Go installed. Output: build/Smugshot.exe (and an ARM one).
# With a version (./scripts/build-windows.sh 0.3.0) it also zips the exe to build/Smugshot-<version>-windows.zip,
# the Windows download.
set -euo pipefail
cd "$(dirname "$0")/../windows"
go test ./...
GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui -s -w" -o ../build/Smugshot.exe .
GOOS=windows GOARCH=arm64 go build -ldflags "-H=windowsgui -s -w" -o ../build/Smugshot-arm64.exe .
ls -la ../build/*.exe
if [[ -n "${1:-}" ]]; then
  rm -f "../build/Smugshot-$1-windows.zip"
  (cd ../build && zip -j -q "Smugshot-$1-windows.zip" Smugshot.exe)
  ls -la "../build/Smugshot-$1-windows.zip"
fi
