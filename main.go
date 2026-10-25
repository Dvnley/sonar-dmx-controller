// Command sonar-dmx is the Sonar DMX Controller: a local web UI that drives
// an Art-Net node (the DMX-AN2) over the Mac's USB Ethernet.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/dvnley/sonar-dmx-controller/internal/artnet"
	"github.com/dvnley/sonar-dmx-controller/internal/controller"
	"github.com/dvnley/sonar-dmx-controller/internal/server"
)

const version = "0.3"

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address for the web UI")
	cfgPath := flag.String("config", defaultConfigPath(), "config file (patch, target IP, last values)")
	noBrowser := flag.Bool("no-browser", false, "do not open the web UI in a browser")
	flag.Parse()

	log.Printf("Sonar DMX Controller v%s", version)
	conn, err := artnet.Listen()
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	if !conn.BoundPort {
		log.Printf("warning: UDP %d is in use (another Art-Net app running?), ArtPoll replies may be missed", artnet.Port)
	}

	ctl, err := controller.New(*cfgPath, conn)
	if err != nil {
		log.Fatal(err)
	}
	ctl.BoundPort = conn.BoundPort
	log.Printf("config: %s", *cfgPath)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v (is the controller already running?)", *addr, err)
	}
	url := fmt.Sprintf("http://%s", ln.Addr())
	log.Printf("web UI: %s", url)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go ctl.Run(ctx)

	srv := &http.Server{Handler: server.New(ctl)}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if !*noBrowser {
		openBrowser(url)
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	if err := ctl.Save(); err != nil {
		log.Printf("save config: %v", err)
	}
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "SonarDMX", "config.json")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
