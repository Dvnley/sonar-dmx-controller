package controller

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Show mode: one screen for running the night. It adds movement patterns for
// the moving lights, one speed for everything that runs, and a one-tap way to
// load Danley's rig into the patch.

// MovementInfo describes a movement pattern for the UI.
type MovementInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Movements is the catalog of movement patterns, in the order the UI shows it.
var Movements = []MovementInfo{
	{"circle", "Circle"}, {"sweep", "Side to side"}, {"nod", "Up and down"},
	{"figure8", "Figure 8"}, {"wander", "Wander"},
}

// MoveRun is one running movement pattern on some moving lights. It moves
// around where the lights are aimed (their pan/tilt values).
type MoveRun struct {
	Fixtures []int   `json:"fixtures"`
	Pattern  string  `json:"pattern"`
	Speed    float64 `json:"speed"` // 0-100, same scale as the programs
	Size     float64 `json:"size"`  // 0-100: how far from the aim it moves
	Music    bool    `json:"music"`
	phase    float64 // in pattern cycles
	last     time.Time
}

// beatsPerCycle: a movement cycle takes 16 beats, so at the middle speed
// (85 BPM) a circle takes about 11 seconds.
const beatsPerCycle = 16

// ShowState is the show part of GET /api/state.
type ShowState struct {
	Speed     float64        `json:"speed"`
	Music     bool           `json:"music"`
	BPM       float64        `json:"bpm"` // the free-running tempo for Speed
	Moves     []MoveRun      `json:"moves"`
	Movements []MovementInfo `json:"movements"`
	RigOK     bool           `json:"rig_ok"` // the patch has the spots, wash and spider
}

func validMovement(id string) bool {
	for _, m := range Movements {
		if m.ID == id {
			return true
		}
	}
	return false
}

// axes returns a fixture's channels that a movement drives: x (side to side)
// and y (up and down). A fixture with two tilts and no pan, like a spider
// with two bars, gets both tilts: see moveValues.
func axes(f fixture.Fixture) (pan, tilt []int) {
	for i, ch := range f.Channels {
		switch ch.Type {
		case fixture.Pan:
			pan = append(pan, i)
		case fixture.Tilt:
			tilt = append(tilt, i)
		}
	}
	return pan, tilt
}

func moves(f fixture.Fixture) bool {
	p, t := axes(f)
	return len(p)+len(t) > 0
}

// patternXY is where a pattern is at phase p (in cycles), each -1..1.
func patternXY(pattern string, p float64) (float64, float64) {
	a := 2 * math.Pi * p
	switch pattern {
	case "circle":
		return math.Cos(a), math.Sin(a)
	case "sweep":
		return math.Sin(a), 0
	case "nod":
		return 0, math.Sin(a)
	case "figure8":
		return math.Sin(a), math.Sin(2 * a)
	case "wander":
		return 0.6*math.Sin(a) + 0.4*math.Sin(2.3*a+1), 0.6*math.Sin(1.7*a+2) + 0.4*math.Sin(0.7*a)
	}
	return 0, 0
}

// StartMovement runs a pattern on the moving lights among targets (nil =
// all), taking them out of any other movement. Re-sending the same pattern
// keeps its place, so changing the speed does not make the lights jump.
func (c *Controller) StartMovement(targets []int, run MoveRun) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !validMovement(run.Pattern) {
		return fmt.Errorf("unknown movement %q", run.Pattern)
	}
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	var fx []int
	for _, i := range idx {
		if moves(c.cfg.Fixtures[i]) {
			fx = append(fx, i)
		}
	}
	if len(fx) == 0 {
		return errors.New("no moving lights in that selection")
	}
	sort.Ints(fx)
	run.Fixtures = fx
	run.Speed = clampF(run.Speed, 0, 100)
	run.Size = clampF(run.Size, 0, 100)
	run.last = c.now()
	for _, r := range c.moves {
		if r.Pattern == run.Pattern && sameInts(r.Fixtures, fx) {
			run.phase = r.phase
		}
	}
	c.dropMoves(fx)
	c.stopMotion()
	c.moves = append(c.moves, &run)
	return nil
}

// StopMovement stops movement on the given fixtures (nil = all). They go
// back to where they are aimed.
func (c *Controller) StopMovement(targets []int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if targets == nil {
		c.moves = nil
		return nil
	}
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	c.dropMoves(idx)
	return nil
}

// dropMoves must be called with mu held.
func (c *Controller) dropMoves(idx []int) {
	drop := map[int]bool{}
	for _, i := range idx {
		drop[i] = true
	}
	var keep []*MoveRun
	for _, r := range c.moves {
		var fx []int
		for _, i := range r.Fixtures {
			if !drop[i] {
				fx = append(fx, i)
			}
		}
		if len(fx) > 0 {
			r.Fixtures = fx
			keep = append(keep, r)
		}
	}
	c.moves = keep
}

// applyMoves moves the patch copy. Called with mu held.
func (c *Controller) applyMoves(fixtures []fixture.Fixture, now time.Time) {
	for _, r := range c.moves {
		dt := now.Sub(r.last).Seconds()
		r.last = now
		if dt < 0 || dt > 1 {
			dt = 0
		}
		bps := (&ProgramRun{Speed: r.Speed}).bpm() / 60
		if r.Music && c.beatLive(now) {
			bps = 1 / c.beats.period.Seconds() * (&ProgramRun{Speed: r.Speed}).musicRate()
		}
		r.phase = math.Mod(r.phase+dt*bps/beatsPerCycle, 1e6)
		amp := clampF(r.Size, 0, 100) / 100 * 127
		n := len(r.Fixtures)
		for k, i := range r.Fixtures {
			if i >= len(fixtures) || !fixtures[i].Enabled {
				continue
			}
			// lights in one run are spread a little apart in the pattern
			x, y := patternXY(r.Pattern, r.phase+float64(k)/float64(max(n, 1))*0.25)
			f := fixtures[i]
			f.Values = append(fixture.Values(nil), f.Values...)
			moveValues(&f, x, y, amp)
			fixtures[i] = f
		}
	}
}

// moveValues offsets the fixture's pan and tilt from where they are aimed.
// A light with no pan but two tilts (a spider's two bars) turns x into the
// bars swinging against each other and y into them swinging together.
func moveValues(f *fixture.Fixture, x, y, amp float64) {
	pan, tilt := axes(*f)
	set := func(i int, d float64) {
		f.Values[i] = uint8(clampF(math.Round(float64(f.Values[i])+d*amp), 0, 255))
	}
	if len(pan) == 0 && len(tilt) >= 2 {
		for k, i := range tilt {
			sign := 1.0
			if k%2 == 0 {
				sign = -1
			}
			set(i, clampF((y+sign*x)*0.7, -1, 1))
		}
		return
	}
	for _, i := range pan {
		set(i, x)
	}
	for _, i := range tilt {
		set(i, y)
	}
}

// SetShowSpeed sets the speed (0-100) and music mode of everything running:
// the programs and the movements. Nothing restarts.
func (c *Controller) SetShowSpeed(speed float64, music bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	speed = clampF(speed, 0, 100)
	c.showSpeed, c.showMusic, c.showSpeedSet = speed, music, true
	for _, r := range c.runs {
		r.rebase(now)
		r.Speed, r.Music = speed, music
	}
	for _, r := range c.moves {
		r.Speed, r.Music = speed, music
	}
}

// ShowGroup switches lights on or off and sets their brightness (0-100).
// Turning on a light whose own brightness channel is at zero opens it fully,
// so "on" always means you can see it.
func (c *Controller) ShowGroup(targets []int, on *bool, level *int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	for _, i := range idx {
		f := &c.cfg.Fixtures[i]
		if on != nil {
			f.Enabled = *on
		}
		if level != nil {
			f.Brightness = clamp(*level, 0, 100)
		}
		if f.Enabled {
			if d := f.Find(fixture.Dimmer); d >= 0 && f.Values[d] == 0 {
				f.Values[d] = 255
			}
		}
	}
	c.changed()
	return nil
}

// SetupRig loads Danley's rig into the patch: gobo spots at 1, the Chauvet
// wash at 17 and the spider at 34, with the uplights switched off.
func (c *Controller) SetupRig() error {
	if err := c.SetPatch(RigFixtures()); err != nil {
		return err
	}
	c.mu.Lock()
	c.moves = nil
	c.mu.Unlock()
	return nil
}

// rigOK must be called with mu held.
func (c *Controller) rigOK() bool {
	want := map[string]int{"gobo-spot-11ch": 1, "wash-fx": 17, "spider-10ch": 34}
	for _, f := range c.cfg.Fixtures {
		if a, ok := want[f.Profile]; ok && a == f.Address && f.Universe == 0 {
			delete(want, f.Profile)
		}
	}
	return len(want) == 0
}

func (c *Controller) showState() ShowState {
	s := ShowState{Speed: 50, Moves: []MoveRun{}, Movements: Movements, RigOK: c.rigOK()}
	if c.showSpeedSet {
		s.Speed, s.Music = c.showSpeed, c.showMusic
	} else if len(c.runs) > 0 {
		s.Speed, s.Music = c.runs[0].Speed, c.runs[0].Music
	}
	s.BPM = math.Round((&ProgramRun{Speed: s.Speed}).bpm())
	for _, r := range c.moves {
		run := *r
		run.Fixtures = append([]int(nil), r.Fixtures...)
		s.Moves = append(s.Moves, run)
	}
	return s
}
