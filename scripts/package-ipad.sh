#!/bin/bash
# Packs the iPad kit into dist/Sonar_DMX_iPad_v0.3: the Xcode project with a
# prebuilt Sonar.xcframework (so the Mac needs only Xcode, not Go), plus an
# unsigned .ipa for sideloading tools. Pass the folder holding "Sonar DMX.app".
set -euo pipefail
cd "$(dirname "$0")/.."
products=${1:?usage: package-ipad.sh <products dir>}
out=dist/Sonar_DMX_iPad_v0.3
rm -rf "$out"
mkdir -p "$out/Payload"
cp -R ios/SonarDMX ios/SonarDMX.xcodeproj ios/Sonar.xcframework "$out/"
rm -rf "$out/SonarDMX.xcodeproj/xcuserdata" "$out/SonarDMX.xcodeproj/project.xcworkspace/xcuserdata"
cp -R "$products/Sonar DMX.app" "$out/Payload/"
(cd "$out" && zip -qry "Sonar DMX unsigned.ipa" Payload && rm -rf Payload)
cp ios/README.md "$out/README.md"
echo "Built $out:"
ls -l "$out"
