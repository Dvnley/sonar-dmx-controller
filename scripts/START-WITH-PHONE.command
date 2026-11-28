#!/bin/bash
# Double-click to start Sonar DMX Controller and control it from a phone or
# iPad. The phone must be on the same Wi-Fi as this Mac; with no Wi-Fi, turn
# on the phone's Personal Hotspot and join it from the Mac's Wi-Fi menu (the
# lights keep working through the cable to the AN2). The address and PIN for
# the phone are printed below and shown on the Show tab under "Phone".
cd "$(dirname "$0")" || exit 1
if [ "$(uname -m)" = "arm64" ]; then bin=./sonar-dmx-mac-arm64; else bin=./sonar-dmx-mac-intel; fi
xattr -d com.apple.quarantine "$bin" 2>/dev/null
chmod +x "$bin"
echo "Starting Sonar DMX Controller with the phone remote. Close this window (or press Ctrl+C) to stop it."
# caffeinate keeps the Mac awake while the app runs, so a schedule keeps playing.
exec caffeinate -i "$bin" -remote "$@"
