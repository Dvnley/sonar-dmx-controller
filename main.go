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
	"strings"
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
	remote := flag.Bool("remote", false, "also serve the UI to other devices on the network (iPad), protected by a PIN")
	pin := flag.String("pin", "", "PIN for -remote (default: one generated and saved in the config)")
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

	listen := *addr
	var opts server.Options
	if *remote {
		_, port, err := net.SplitHostPort(*addr)
		if err != nil {
			log.Fatalf("bad -addr %q: %v", *addr, err)
		}
		listen = net.JoinHostPort("", port)
		opts.PIN = *pin
		if opts.PIN == "" {
			opts.PIN = ctl.RemotePIN()
		}
		if host, err := os.Hostname(); err == nil && host != "" {
			if !strings.HasSuffix(host, ".local") {
				host += ".local"
			}
			opts.URLs = append(opts.URLs, fmt.Sprintf("http://%s", net.JoinHostPort(host, port)))
		}
		target := net.ParseIP(ctl.State().TargetIP)
		for _, n := range artnet.LocalIPv4s() {
			if n.Contains(target) {
				continue // the Art-Net link, not the Wi-Fi the iPad is on
			}
			opts.URLs = append(opts.URLs, fmt.Sprintf("http://%s", net.JoinHostPort(n.IP.String(), port)))
		}
	}

	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v (is the controller already running?)", listen, err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	url := "http://127.0.0.1:" + port
	log.Printf("web UI: %s", url)
	if *remote {
		log.Printf("iPad remote is on. On the iPad, open one of these in Safari (same Wi-Fi as this Mac):")
		for _, u := range opts.URLs {
			log.Printf("    %s", u)
		}
		log.Printf("PIN: %s", opts.PIN)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go ctl.Run(ctx)

	srv := &http.Server{Handler: server.New(ctl, opts)}
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
