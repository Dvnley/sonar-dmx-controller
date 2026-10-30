// Package sonar runs the whole controller inside the iPad app. The app binds
// it with gomobile (Sonar.xcframework) and shows the returned URL in a web
// view, so the iPad uses the same UI and sends Art-Net itself.
package sonar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/artnet"
	"github.com/dvnley/sonar-dmx-controller/internal/controller"
	"github.com/dvnley/sonar-dmx-controller/internal/server"
)

// Port is tried first so the web view keeps the same origin (and its saved
// layouts) between launches.
const Port = 18080

var (
	mu      sync.Mutex
	running *instance
)

type instance struct {
	url    string
	ctl    *controller.Controller
	conn   *artnet.Conn
	srv    *http.Server
	cancel context.CancelFunc
}

// Start runs the controller with its config in dataDir and returns the URL of
// the UI. Calling it again while it runs returns the same URL, unless iOS
// closed the sockets while the app was in the background, in which case it
// starts over with the saved config.
func Start(dataDir string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		if alive(running.url) {
			return running.url, nil
		}
		log.Printf("controller stopped answering, restarting")
		running.stop()
		running = nil
	}

	conn, err := artnet.Listen()
	if err != nil {
		return "", err
	}
	ctl, err := controller.New(filepath.Join(dataDir, "config.json"), conn)
	if err != nil {
		conn.Close()
		return "", err
	}
	ctl.BoundPort = conn.BoundPort

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", Port))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		conn.Close()
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ctl.Run(ctx)
	srv := &http.Server{Handler: server.New(ctl, server.Options{})}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("web UI: %v", err)
		}
	}()
	running = &instance{url: "http://" + ln.Addr().String(), ctl: ctl, conn: conn, srv: srv, cancel: cancel}
	return running.url, nil
}

// Save writes the config now. The app calls it when it goes to the background.
func Save() error {
	mu.Lock()
	defer mu.Unlock()
	if running == nil {
		return nil
	}
	return running.ctl.Save()
}

// Stop saves and shuts everything down.
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		running.stop()
		running = nil
	}
}

func (in *instance) stop() {
	in.cancel()
	_ = in.srv.Close()
	_ = in.conn.Close()
	if err := in.ctl.Save(); err != nil {
		log.Printf("save config: %v", err)
	}
}

func alive(url string) bool {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(url + "/api/state")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
