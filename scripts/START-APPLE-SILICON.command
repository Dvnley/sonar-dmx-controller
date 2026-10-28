#!/bin/bash
# Double-click to start Sonar DMX Controller on an Apple Silicon (M1/M2/M3/M4) Mac.
cd "$(dirname "$0")" || exit 1
xattr -d com.apple.quarantine ./sonar-dmx-mac-arm64 2>/dev/null
chmod +x ./sonar-dmx-mac-arm64
echo "Starting Sonar DMX Controller. Close this window (or press Ctrl+C) to stop it."
exec ./sonar-dmx-mac-arm64 "$@"
