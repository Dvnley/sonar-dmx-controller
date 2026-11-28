package controller

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Everyday tools for running the rig: aiming the spots, color hits, going
// back to the last look, a light check before a gig, and Auto from the
// schedule.

// MoodAuto is the schedule's "Auto" entry: the music (or, with no mic, the
// clock) runs the show.
const MoodAuto = "auto"

// AimPoint is a saved place for the spots to point at.
type AimPoint struct {
	Name string  `json:"name"`
	X    float64 `json:"x"` // 0-1, left to right
	Y    float64 `json:"y"` // 0-1, the up/down channel
}

// HitColors are the colors a hold-to-hit button can flash.
var HitColors = []string{"white", "red", "green", "blue", "uv", "amber"}

// SetHit holds every light (or the targets) in one color while on, like
// Flash in a color. color "" is white.
func (c *Controller) SetHit(on bool, color string, targets []int) error {
	if color == "" {
		color = "white"
	}
	ok := false
	for _, h := range HitColors {
		ok = ok || h == color
	}
	if !ok {
		return fmt.Errorf("unknown color %q", color)
	}
	c.SetFlash(on, targets)
	c.mu.Lock()
	c.flashColor = color
	c.mu.Unlock()
	return nil
}

// SetAim points the moving lights among targets (nil = all) at x, y (0-1).
// Movements keep running around the new aim.
func (c *Controller) SetAim(targets []int, x, y float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	n := 0
	for _, i := range idx {
		f := &c.cfg.Fixtures[i]
		if !moves(*f) {
			continue
		}
		pan, tilt := axes(*f)
		for _, j := range pan {
			f.Values[j] = uint8(math.Round(clampF(x, 0, 1) * 255))
		}
		for _, j := range tilt {
			f.Values[j] = uint8(math.Round(clampF(y, 0, 1) * 255))
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("no moving lights to aim")
	}
	c.changed()
	return nil
}

// aimOf must be called with mu held: where the first moving light points.
func (c *Controller) aimOf() (float64, float64, bool) {
	for _, f := range c.cfg.Fixtures {
		if !moves(f) {
			continue
		}
		pan, tilt := axes(f)
		x, y := 0.5, 0.5
		if len(pan) > 0 {
			x = float64(f.Values[pan[0]]) / 255
		}
		if len(tilt) > 0 {
			y = float64(f.Values[tilt[0]]) / 255
		}
		return x, y, true
	}
	return 0, 0, false
}

// SaveAim stores where the spots point now under a name (replacing one of
// the same name), or removes it when remove is set.
func (c *Controller) SaveAim(name string, remove bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("give the spot a name")
	}
	var keep []AimPoint
	for _, p := range c.cfg.AimPoints {
		if !strings.EqualFold(p.Name, name) {
			keep = append(keep, p)
		}
	}
	if !remove {
		x, y, ok := c.aimOf()
		if !ok {
			return fmt.Errorf("no moving lights to aim")
		}
		if len(keep) >= 12 {
			return fmt.Errorf("up to 12 saved spots")
		}
		keep = append(keep, AimPoint{Name: name, X: math.Round(x*1000) / 1000, Y: math.Round(y*1000) / 1000})
	}
	c.cfg.AimPoints = keep
	c.changed()
	return nil
}

// Undo goes back to the look that was on before the last look was picked.
// Pressing it again goes forward again.
func (c *Controller) Undo() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.undo == nil {
		return fmt.Errorf("nothing to go back to yet")
	}
	back := *c.undo
	now := c.captureMood()
	now.Name = c.playing
	c.auto.on = false
	c.playMood(&back, back.ID, 1)
	c.playing = back.Name
	c.undo = &now
	return nil
}

// rememberForUndo must be called with mu held, before a look is changed by hand.
func (c *Controller) rememberForUndo() {
	m := c.captureMood()
	m.ID, m.Name = c.playingID, c.playing
	if m.Name == "" {
		m.Name = "Before"
	}
	c.undo = &m
}

// ---------- light check ----------

// checkStep is one step of the light check: one light doing one thing.
type checkStep struct {
	fixture int
	label   string
	color   string  // for color lights
	x, y    float64 // for moving lights
}

const checkStepTime = 1200 * time.Millisecond

// checkSteps must be called with mu held.
func (c *Controller) checkSteps() []checkStep {
	var steps []checkStep
	for i, f := range c.cfg.Fixtures {
		if f.HasColor() {
			for _, col := range []string{"red", "green", "blue", "white"} {
				steps = append(steps, checkStep{fixture: i, label: col, color: col})
			}
		}
		if moves(f) {
			for _, p := range []struct {
				l    string
				x, y float64
			}{{"center", .5, .5}, {"left", .2, .5}, {"right", .8, .5}, {"up", .5, .2}, {"down", .5, .8}, {"center", .5, .5}} {
				steps = append(steps, checkStep{fixture: i, label: p.l, x: p.x, y: p.y})
			}
		}
	}
	return steps
}

// StartCheck runs every light through its colors and moves, one light at a
// time, so you can see before a gig that each one answers. stop ends it.
func (c *Controller) StartCheck(stop bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if stop {
		c.checkFrom = time.Time{}
		return
	}
	c.checkFrom = c.now()
}

// checkNow must be called with mu held: the current step, or nil.
func (c *Controller) checkNow(now time.Time) (*checkStep, int, int) {
	if c.checkFrom.IsZero() {
		return nil, 0, 0
	}
	steps := c.checkSteps()
	i := int(now.Sub(c.checkFrom) / checkStepTime)
	if i >= len(steps) {
		c.checkFrom = time.Time{}
		return nil, 0, 0
	}
	return &steps[i], i, len(steps)
}

// checkLook must be called with mu held: everything off except the light
// being checked.
func checkLook(src []fixture.Fixture, s *checkStep) []fixture.Fixture {
	out := make([]fixture.Fixture, len(src))
	for i, f := range src {
		f.Values = append(fixture.Values(nil), f.Values...)
		f.Enabled = i == s.fixture
		f.Brightness = 100
		if i == s.fixture {
			if s.color != "" {
				f = testLook([]fixture.Fixture{f}, s.color)[0]
			} else {
				pan, tilt := axes(f)
				for _, j := range pan {
					f.Values[j] = uint8(s.x * 255)
				}
				for _, j := range tilt {
					f.Values[j] = uint8(s.y * 255)
				}
				f.Set(fixture.Dimmer, 255)
				f.Set(fixture.Shutter, 0)
			}
		}
		out[i] = f
	}
	return out
}

// CheckState is the light check part of GET /api/state.
type CheckState struct {
	Running bool   `json:"running"`
	Light   string `json:"light,omitempty"`
	Doing   string `json:"doing,omitempty"`
	Step    int    `json:"step"`
	Steps   int    `json:"steps"`
}

// ToolsState is the tools part of GET /api/state.
type ToolsState struct {
	Check     CheckState `json:"check"`
	AimPoints []AimPoint `json:"aim_points"`
	Aim       *AimPoint  `json:"aim,omitempty"`
	CanUndo   bool       `json:"can_undo"`
	UndoName  string     `json:"undo_name,omitempty"`
	HitColor  string     `json:"hit_color,omitempty"`
}

// toolsState must be called with mu held.
func (c *Controller) toolsState() ToolsState {
	s := ToolsState{AimPoints: append([]AimPoint{}, c.cfg.AimPoints...), CanUndo: c.undo != nil}
	if c.undo != nil {
		s.UndoName = c.undo.Name
	}
	if x, y, ok := c.aimOf(); ok {
		s.Aim = &AimPoint{X: x, Y: y}
	}
	if !c.flashUntil.IsZero() {
		s.HitColor = c.flashColor
	}
	if st, i, n := c.checkNow(c.now()); st != nil {
		s.Check = CheckState{Running: true, Light: c.cfg.Fixtures[st.fixture].Name, Doing: st.label, Step: i + 1, Steps: n}
	}
	return s
}
