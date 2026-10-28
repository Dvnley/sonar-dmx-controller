// Package server is the HTTP API and the embedded web UI.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"

	"github.com/dvnley/sonar-dmx-controller/internal/controller"
	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

//go:embed web
var webFS embed.FS

// New returns the handler for the API and the UI.
func New(c *controller.Controller, opts Options) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(static)))

	// v0.2 endpoints
	mux.HandleFunc("GET /api/diag", func(w http.ResponseWriter, r *http.Request) { reply(w, c.Diag()) })
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { reply(w, c.State()) })
	mux.HandleFunc("POST /api/test", func(w http.ResponseWriter, r *http.Request) {
		c.StartTest()
		reply(w, c.State())
	})
	mux.HandleFunc("POST /api/blackout", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On *bool `json:"on"`
		}
		if !decode(w, r, &body) {
			return
		}
		c.SetBlackout(body.On)
		reply(w, c.State())
	})
	mux.HandleFunc("POST /api/color", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Fixtures json.RawMessage `json:"fixtures"`
			R        *int            `json:"r"`
			G        *int            `json:"g"`
			B        *int            `json:"b"`
			A        *int            `json:"a"`
			W        *int            `json:"w"`
			UV       *int            `json:"uv"`
			Dimmer   *int            `json:"dimmer"`
			Fade     float64         `json:"fade"`
		}
		if !decode(w, r, &body) {
			return
		}
		targets, err := parseTargets(body.Fixtures)
		if err == nil {
			err = c.SetColor(targets, controller.ColorSet{R: body.R, G: body.G, B: body.B, A: body.A, W: body.W, UV: body.UV, Dimmer: body.Dimmer, Fade: body.Fade})
		}
		result(w, c, err)
	})
	mux.HandleFunc("POST /api/fixture", func(w http.ResponseWriter, r *http.Request) {
		var body controller.FixtureUpdate
		if decode(w, r, &body) {
			result(w, c, c.UpdateFixture(body))
		}
	})
	mux.HandleFunc("POST /api/master", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Value int `json:"value"`
		}
		if decode(w, r, &body) {
			c.SetMaster(body.Value)
			reply(w, c.State())
		}
	})
	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		var body controller.ConfigUpdate
		if decode(w, r, &body) {
			result(w, c, c.UpdateConfig(body))
		}
	})

	// new in this rebuild
	mux.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) { reply(w, fixture.Profiles) })
	mux.HandleFunc("POST /api/patch", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Fixtures []fixture.Fixture `json:"fixtures"`
		}
		if decode(w, r, &body) {
			result(w, c, c.SetPatch(body.Fixtures))
		}
	})
	mux.HandleFunc("POST /api/fixture/reset", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Index int `json:"index"`
		}
		if decode(w, r, &body) {
			result(w, c, c.ResetFixture(body.Index))
		}
	})
	mux.HandleFunc("POST /api/poll", func(w http.ResponseWriter, r *http.Request) {
		c.Poll()
		reply(w, c.Diag())
	})
	mux.HandleFunc("POST /api/sweep", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Universe int                   `json:"universe"`
			Script   string                `json:"script"`
			Manual   bool                  `json:"manual"`
			Plan     *controller.SweepPlan `json:"plan"`
		}
		if !decode(w, r, &body) {
			return
		}
		var steps []controller.Step
		var err error
		switch {
		case body.Plan != nil:
			body.Manual = body.Plan.Manual
			steps, err = body.Plan.Build()
		case body.Script != "":
			steps, err = controller.ParseScript(body.Script)
		default:
			err = errors.New("send a plan or a script")
		}
		if err == nil {
			err = c.StartSweep(controller.Sweep{Universe: body.Universe, Manual: body.Manual, Steps: steps})
		}
		result(w, c, err)
	})
	mux.HandleFunc("POST /api/sweep/stop", func(w http.ResponseWriter, r *http.Request) {
		c.StopSweep()
		reply(w, c.State())
	})
	mux.HandleFunc("POST /api/sweep/step", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Delta int `json:"delta"`
		}
		if decode(w, r, &body) {
			result(w, c, c.StepSweep(body.Delta))
		}
	})
	// live performance
	mux.HandleFunc("POST /api/flash", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On bool `json:"on"`
		}
		if decode(w, r, &body) {
			c.SetFlash(body.On)
			reply(w, c.State())
		}
	})
	mux.HandleFunc("POST /api/strobe", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Hz float64 `json:"hz"`
		}
		if decode(w, r, &body) {
			c.SetStrobe(body.Hz)
			reply(w, c.State())
		}
	})
	mux.HandleFunc("POST /api/rainbow", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Seconds float64 `json:"seconds"`
		}
		if decode(w, r, &body) {
			c.SetRainbow(body.Seconds)
			reply(w, c.State())
		}
	})
	mux.HandleFunc("POST /api/scene/{action}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Slot int     `json:"slot"`
			Name string  `json:"name"`
			Fade float64 `json:"fade"`
		}
		if !decode(w, r, &body) {
			return
		}
		var err error
		switch r.PathValue("action") {
		case "save":
			err = c.SaveScene(body.Slot, body.Name)
		case "recall":
			err = c.RecallScene(body.Slot, body.Fade)
		case "clear":
			err = c.ClearScene(body.Slot)
		default:
			fail(w, http.StatusNotFound, "unknown scene action")
			return
		}
		result(w, c, err)
	})
	mux.HandleFunc("GET /api/remote", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]any{"enabled": opts.PIN != "", "urls": opts.URLs}
		if opts.URLs == nil {
			info["urls"] = []string{}
		}
		if isLocal(r) {
			info["pin"] = opts.PIN
		}
		reply(w, info)
	})
	return guard(opts, mux)
}

// parseTargets accepts "all", a missing field, a list of indexes, or one index.
func parseTargets(raw json.RawMessage) ([]int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "all" {
			return nil, nil
		}
		return nil, errors.New(`fixtures must be "all" or a list of indexes`)
	}
	var list []int
	if json.Unmarshal(raw, &list) == nil {
		return list, nil
	}
	var one int
	if json.Unmarshal(raw, &one) == nil {
		return []int{one}, nil
	}
	return nil, errors.New(`fixtures must be "all" or a list of indexes`)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return false
	}
	return true
}

func result(w http.ResponseWriter, c *controller.Controller, err error) {
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	reply(w, c.State())
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
