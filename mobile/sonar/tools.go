//go:build tools

package sonar

// Keeps golang.org/x/mobile in go.mod: gomobile bind needs it to build
// Sonar.xcframework.
import _ "golang.org/x/mobile/bind"
