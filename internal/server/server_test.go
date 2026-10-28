package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/artnet"
	"github.com/dvnley/sonar-dmx-controller/internal/controller"
)

type nopNet struct{}

func (nopNet) SendDmx(string, int, []byte) error { return nil }
func (nopNet) Poll(...string) error              { return nil }
func (nopNet) Seen(string, time.Duration) bool   { return true }
func (nopNet) Nodes() []artnet.Node              { return nil }

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestV02Endpoints(t *testing.T) {
	c, _ := controller.New("", nopNet{})
	h := New(c, Options{})
	if code, d := do(t, h, "GET", "/api/diag", ""); code != 200 || d["an2_online"] != true || d["target"] != "2.0.0.1" {
		t.Fatalf("diag: %d %v", code, d)
	}
	for _, tc := range []struct{ path, body string }{
		{"/api/test", "{}"},
		{"/api/blackout", "{}"},
		{"/api/blackout", ""},
		{"/api/color", `{"fixtures":"all","r":255,"g":0,"b":0,"a":0,"w":0,"uv":0}`},
		{"/api/color", `{"fixtures":[0,2],"b":255}`},
		{"/api/fixture", `{"index":1,"brightness":40}`},
		{"/api/fixture", `{"index":1,"enabled":false}`},
		{"/api/master", `{"value":80}`},
		{"/api/config", `{"target_ip":"2.0.0.1"}`},
		{"/api/sweep", `{"plan":{"from":1,"to":9,"value":255,"manual":true}}`},
		{"/api/sweep/step", `{"delta":1}`},
		{"/api/sweep/stop", "{}"},
	} {
		if code, out := do(t, h, "POST", tc.path, tc.body); code != 200 {
			t.Errorf("%s %s: %d %v", tc.path, tc.body, code, out)
		}
	}
	_, s := do(t, h, "GET", "/api/state", "")
	if vals, ok := s["fixtures"].([]any)[0].(map[string]any)["values"].([]any); !ok || vals[0] != 255.0 {
		t.Errorf("values should be a JSON number array, got %#v", s["fixtures"].([]any)[0].(map[string]any)["values"])
	}
	if s["master"] != 80.0 || s["blackout"] != false {
		t.Errorf("state: master %v blackout %v", s["master"], s["blackout"])
	}
	for _, bad := range []struct{ path, body string }{
		{"/api/color", `{"fixtures":[99],"r":1}`},
		{"/api/fixture", `{"index":-1}`},
		{"/api/config", `{"target_ip":"nope"}`},
		{"/api/sweep", `{}`},
		{"/api/master", `{bad`},
	} {
		if code, _ := do(t, h, "POST", bad.path, bad.body); code != 400 {
			t.Errorf("%s %s: got %d, want 400", bad.path, bad.body, code)
		}
	}
}

func TestUIServed(t *testing.T) {
	c, _ := controller.New("", nopNet{})
	h := New(c, Options{})
	for _, p := range []string{"/", "/app.js", "/style.css"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 || rec.Body.Len() < 100 {
			t.Errorf("%s: %d (%d bytes)", p, rec.Code, rec.Body.Len())
		}
	}
}

func TestRemotePIN(t *testing.T) {
	c, _ := controller.New("", nopNet{})
	h := New(c, Options{PIN: "123456", URLs: []string{"http://mac.local:8080"}})
	get := func(path, remote, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = remote
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: pinCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("/api/state", "127.0.0.1:5000", ""); rec.Code != 200 {
		t.Errorf("Mac itself should not need a PIN, got %d", rec.Code)
	}
	if rec := get("/api/state", "192.168.1.20:5000", ""); rec.Code != 401 {
		t.Errorf("iPad without PIN: got %d, want 401", rec.Code)
	}
	if rec := get("/", "192.168.1.20:5000", ""); !strings.Contains(rec.Body.String(), `action="/login"`) {
		t.Error("iPad without PIN should get the login page")
	}
	if rec := get("/api/state", "192.168.1.20:5000", "123456"); rec.Code != 200 {
		t.Errorf("iPad with PIN cookie: got %d", rec.Code)
	}

	req := httptest.NewRequest("POST", "/login", strings.NewReader("pin=123456"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.168.1.20:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Set-Cookie"), pinCookie+"=123456") {
		t.Errorf("login: %d %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}

	var info map[string]any
	_ = json.Unmarshal(get("/api/remote", "127.0.0.1:5000", "").Body.Bytes(), &info)
	if info["pin"] != "123456" || info["enabled"] != true {
		t.Errorf("remote info on the Mac: %v", info)
	}
	info = nil
	_ = json.Unmarshal(get("/api/remote", "192.168.1.20:5000", "123456").Body.Bytes(), &info)
	if _, ok := info["pin"]; ok {
		t.Error("PIN must not be sent to other devices")
	}
}
