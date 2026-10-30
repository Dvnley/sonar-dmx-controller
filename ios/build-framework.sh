#!/bin/bash
# Builds Sonar.xcframework (the Go controller) next to the Xcode project.
# Needs Xcode and Go (https://go.dev/dl/ or `brew install go`).
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PATH:$(go env GOPATH)/bin"
version=$(go list -m -f '{{.Version}}' golang.org/x/mobile)
go install "golang.org/x/mobile/cmd/gomobile@$version" "golang.org/x/mobile/cmd/gobind@$version"
gomobile init
rm -rf ios/Sonar.xcframework
gomobile bind -target ios,iossimulator -iosversion 16.0 -trimpath -ldflags="-s -w" -o ios/Sonar.xcframework ./mobile/sonar
echo "Built ios/Sonar.xcframework. Now open ios/SonarDMX.xcodeproj in Xcode."
