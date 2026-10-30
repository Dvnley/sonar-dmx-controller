package sonar

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestStartStop(t *testing.T) {
	dir := t.TempDir()
	url, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer Stop()
	resp, err := http.Get(url + "/api/state")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state: %s", resp.Status)
	}
	again, err := Start(dir)
	if err != nil || again != url {
		t.Fatalf("second Start = %q, %v; want %q", again, err, url)
	}
	Stop()
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config not saved: %v", err)
	}
	// After a stop (or iOS closing the sockets) Start brings it back.
	url, err = Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !alive(url) {
		t.Fatal("not answering after restart")
	}
}
