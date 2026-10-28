#!/bin/bash
# Builds both Mac binaries into dist/Sonar_DMX_Controller_v0.3 with the START scripts.
set -euo pipefail
cd "$(dirname "$0")/.."
out=dist/Sonar_DMX_Controller_v0.3
rm -rf "$out"
mkdir -p "$out"
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$out/sonar-dmx-mac-arm64" .
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$out/sonar-dmx-mac-intel" .
cp scripts/START-APPLE-SILICON.command scripts/START-INTEL.command scripts/START-WITH-IPAD.command "$out/"
chmod +x "$out"/*.command "$out"/sonar-dmx-mac-*
cp README.md "$out/README.md"
echo "Built $out:"
ls -l "$out"
