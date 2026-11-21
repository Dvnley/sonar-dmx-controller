#!/bin/bash
# Double-click to start Sonar DMX Controller on an Intel Mac.
cd "$(dirname "$0")" || exit 1
xattr -d com.apple.quarantine ./sonar-dmx-mac-intel 2>/dev/null
chmod +x ./sonar-dmx-mac-intel
echo "Starting Sonar DMX Controller. Close this window (or press Ctrl+C) to stop it."
# caffeinate keeps the Mac awake while the app runs, so a schedule keeps playing.
exec caffeinate -i ./sonar-dmx-mac-intel "$@"
