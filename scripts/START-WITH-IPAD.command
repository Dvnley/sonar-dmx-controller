#!/bin/bash
# Double-click to start Sonar DMX Controller and let an iPad on the same
# Wi-Fi control it. The addresses and PIN for the iPad are printed below
# and shown in the app's Setup tab.
cd "$(dirname "$0")" || exit 1
if [ "$(uname -m)" = "arm64" ]; then bin=./sonar-dmx-mac-arm64; else bin=./sonar-dmx-mac-intel; fi
xattr -d com.apple.quarantine "$bin" 2>/dev/null
chmod +x "$bin"
echo "Starting Sonar DMX Controller with iPad remote. Close this window (or press Ctrl+C) to stop it."
exec "$bin" -remote "$@"
