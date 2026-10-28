package server

import (
	"crypto/subtle"
	"html/template"
	"net"
	"net/http"
	"time"
)

// Options configure access from other devices on the network.
type Options struct {
	// PIN is required from any client that is not this Mac. Leave it empty
	// only when the listener is bound to loopback.
	PIN string
	// URLs are the addresses another device can use to reach the UI.
	URLs []string
}

const pinCookie = "sonar_pin"

func isLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func pinOK(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// guard lets this Mac straight in and asks other devices for the PIN once,
// then remembers it in a cookie.
func guard(opts Options, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.PIN == "" || isLocal(r) {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/login" && r.Method == http.MethodPost {
			if pinOK(r.FormValue("pin"), opts.PIN) {
				http.SetCookie(w, &http.Cookie{
					Name: pinCookie, Value: opts.PIN, Path: "/", HttpOnly: true,
					SameSite: http.SameSiteLaxMode, MaxAge: 365 * 24 * 3600,
				})
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			time.Sleep(time.Second) // slow down guessing
			loginPage(w, http.StatusUnauthorized, "Wrong PIN. It is shown in the Setup tab on the Mac.")
			return
		}
		if c, err := r.Cookie(pinCookie); err == nil && pinOK(c.Value, opts.PIN) {
			next.ServeHTTP(w, r)
			return
		}
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			fail(w, http.StatusUnauthorized, "enter the PIN first")
			return
		}
		loginPage(w, http.StatusOK, "")
	})
}

var loginTmpl = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="apple-mobile-web-app-capable" content="yes">
<title>Sonar DMX</title>
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0e1014;color:#e6e8ee;font:16px -apple-system,BlinkMacSystemFont,sans-serif}
form{background:#171a21;border:1px solid #2a2f3a;border-radius:12px;padding:24px;width:min(320px,90vw)}
h1{font-size:20px;margin:0 0 6px}p{color:#8b92a3;font-size:14px}
input,button{font:inherit;width:100%;box-sizing:border-box;padding:12px;border-radius:8px;border:1px solid #2a2f3a;margin-top:10px}
input{background:#222733;color:#e6e8ee;font-size:24px;letter-spacing:6px;text-align:center}
button{background:#4f8cff;border-color:#4f8cff;color:#fff;font-weight:600}
.err{color:#ff4d5a}
</style></head><body>
<form method="post" action="/login">
<h1>Sonar DMX</h1>
<p>Enter the PIN shown in the Setup tab on the Mac.</p>
{{if .}}<p class="err">{{.}}</p>{{end}}
<input name="pin" inputmode="numeric" autocomplete="one-time-code" autofocus required>
<button>Connect</button>
</form></body></html>`))

func loginPage(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = loginTmpl.Execute(w, msg)
}
