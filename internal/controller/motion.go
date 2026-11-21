package controller

import (
	"fmt"
	"math"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Moving moods: a mood can hold marked points (snapshots of the whole rig)
// and glide from one to the next in a loop. A speed slider sets how fast it
// moves; with "follow the beat" it moves a set number of beats per point,
// and "pulse" bumps the level on every beat of the music (TAP or mic).

// MoodPoint is one marked point of a moving mood.
type MoodPoint struct {
	Fixtures []SceneFixture `json:"fixtures"`
}

// Motion is how a mood moves between its points and reacts to the music.
type Motion struct {
	Speed float64 `json:"speed"` // 0-100: 60 s per point down to 0.5 s
	Hold  float64 `json:"hold"`  // 0-100: share of each step that rests on the point
	Beat  bool    `json:"beat"`  // move on the music beat instead of the clock
	Pulse float64 `json:"pulse"` // 0-100: how much the level dips between beats
}

// MaxPoints keeps a moving mood to a sensible size.
const MaxPoints = 32

// cycle is the moving mood that is playing.
type cycle struct {
	mood   string
	points []MoodPoint
	motion Motion
	phase  float64 // points travelled, fractional
	last   time.Time
}

// secondsPerPoint is the free-running time between points.
func (m Motion) secondsPerPoint() float64 {
	return 60 * math.Pow(0.5/60, clampF(m.Speed, 0, 100)/100)
}

// beatsPerPoint maps the speed slider to 16, 8, 4, 2 or 1 beats per point.
func (m Motion) beatsPerPoint() float64 {
	return math.Pow(2, 4-math.Round(clampF(m.Speed, 0, 100)/25))
}

// MotionLabel describes the speed for the UI.
func (m Motion) MotionLabel() string {
	if m.Beat {
		n := m.beatsPerPoint()
		if n == 1 {
			return "every beat"
		}
		return fmt.Sprintf("every %g beats", n)
	}
	s := m.secondsPerPoint()
	if s >= 10 {
		return fmt.Sprintf("%.0f s per point", s)
	}
	return fmt.Sprintf("%.1f s per point", s)
}

// smoothType reports whether a channel glides between points. Wheels, gobos,
// shutters and the like jump halfway instead of scrolling through every slot.
func smoothType(t fixture.ChannelType) bool {
	switch t {
	case fixture.Red, fixture.Green, fixture.Blue, fixture.Amber, fixture.White, fixture.UV,
		fixture.Dimmer, fixture.Pan, fixture.Tilt, fixture.PanFine, fixture.TiltFine, fixture.Focus:
		return true
	}
	return false
}

// startCycle must be called with mu held.
func (c *Controller) startCycle(m Mood, now time.Time) {
	if len(m.Points) < 2 {
		c.cycle = nil
		return
	}
	c.cycle = &cycle{mood: m.ID, points: m.Points, motion: m.Motion, last: now}
}

// applyCycle moves the patch copy along the points. Called with mu held,
// before the programs so a program can still run on top.
func (c *Controller) applyCycle(fixtures []fixture.Fixture, now time.Time) {
	cy := c.cycle
	if cy == nil || len(cy.points) < 2 {
		return
	}
	dt := now.Sub(cy.last).Seconds()
	cy.last = now
	if dt < 0 || dt > 1 {
		dt = 0
	}
	rate := 1 / cy.motion.secondsPerPoint()
	if cy.motion.Beat && c.beatLive(now) {
		rate = 1 / (c.beats.period.Seconds() * cy.motion.beatsPerPoint())
	}
	cy.phase = math.Mod(cy.phase+dt*rate, float64(len(cy.points)))
	k := int(cy.phase)
	a, b := cy.points[k], cy.points[(k+1)%len(cy.points)]
	// rest on each point for the hold share of the step, then glide
	hold := clampF(cy.motion.Hold, 0, 95) / 100
	t := (cy.phase - float64(k) - hold) / (1 - hold)
	t = clampF(t, 0, 1)
	t = t * t * (3 - 2*t)
	for i := range fixtures {
		if i >= len(a.Fixtures) || i >= len(b.Fixtures) {
			break
		}
		pa, pb := a.Fixtures[i], b.Fixtures[i]
		f := fixtures[i]
		if len(pa.Values) != len(f.Values) || len(pb.Values) != len(f.Values) {
			continue
		}
		vals := make(fixture.Values, len(f.Values))
		for j, ch := range f.Channels {
			va, vb := float64(pa.Values[j]), float64(pb.Values[j])
			if !pa.Enabled {
				va = vb
			} else if !pb.Enabled {
				vb = va
			}
			switch {
			case smoothType(ch.Type):
				vals[j] = uint8(math.Round(va + (vb-va)*t))
			case t < 0.5:
				vals[j] = uint8(va)
			default:
				vals[j] = uint8(vb)
			}
		}
		la, lb := 0.0, 0.0
		if pa.Enabled {
			la = float64(pa.Brightness)
		}
		if pb.Enabled {
			lb = float64(pb.Brightness)
		}
		f.Values = vals
		f.Brightness = int(math.Round(la + (lb-la)*t))
		f.Enabled = f.Brightness > 0
		fixtures[i] = f
	}
}

func (c *Controller) beatLive(now time.Time) bool {
	return c.beats.period > 0 && !c.beats.last.IsZero() && now.Sub(c.beats.last) < 4*time.Second
}

// applyPulse dips the level between beats while music is coming in.
// Called with mu held, after the programs.
func (c *Controller) applyPulse(fixtures []fixture.Fixture, now time.Time) {
	if c.cycle == nil && c.pulse == 0 {
		return
	}
	amount := c.pulse
	if c.cycle != nil {
		amount = c.cycle.motion.Pulse
	}
	if amount <= 0 || !c.beatLive(now) {
		return
	}
	pos := c.beats.position(now)
	frac := pos - math.Floor(pos)
	level := 1 - clampF(amount, 0, 100)/100*(1-math.Exp(-frac*5))
	for i := range fixtures {
		if fixtures[i].HasColor() {
			fixtures[i].Brightness = int(math.Round(float64(fixtures[i].Brightness) * level))
		}
	}
}

// markLook is what the rig shows right now, for marking a point.
// Called with mu held.
func (c *Controller) markLook() MoodPoint {
	src := c.cfg.Fixtures
	if len(c.live) == len(src) {
		src = c.live
	}
	var p MoodPoint
	for _, f := range src {
		p.Fixtures = append(p.Fixtures, SceneFixture{
			Values: append(fixture.Values(nil), f.Values...), Brightness: f.Brightness, Enabled: f.Enabled,
		})
	}
	return p
}

// MarkPoint adds what the lights show right now as the next point of a mood.
func (c *Controller) MarkPoint(id string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return 0, fmt.Errorf("no mood %q", id)
	}
	m := &c.cfg.Moods[i]
	if len(m.Points) >= MaxPoints {
		return len(m.Points), fmt.Errorf("a mood can have up to %d points", MaxPoints)
	}
	m.Points = append(m.Points, c.markLook())
	c.refreshCycle(*m)
	c.changed()
	return len(m.Points), nil
}

// EditPoint removes a point (delta 0) or moves it earlier (-1) or later (+1).
func (c *Controller) EditPoint(id string, index int, remove bool, delta int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	m := &c.cfg.Moods[i]
	if index < 0 || index >= len(m.Points) {
		return fmt.Errorf("no point %d", index+1)
	}
	if remove {
		m.Points = append(m.Points[:index], m.Points[index+1:]...)
	} else {
		j := clamp(index+delta, 0, len(m.Points)-1)
		m.Points[index], m.Points[j] = m.Points[j], m.Points[index]
	}
	c.refreshCycle(*m)
	c.changed()
	return nil
}

// ShowPoint puts one point on the rig so you can see what it looks like.
func (c *Controller) ShowPoint(id string, index int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	m := c.cfg.Moods[i]
	if index < 0 || index >= len(m.Points) {
		return fmt.Errorf("no point %d", index+1)
	}
	c.stopMotion()
	c.runs, c.rainbowSec = nil, 0
	now := c.now()
	for j, sf := range m.Points[index].Fixtures {
		if j >= len(c.cfg.Fixtures) || len(sf.Values) != len(c.cfg.Fixtures[j].Values) {
			continue
		}
		c.beginFade(j, 500*time.Millisecond, now)
		f := &c.cfg.Fixtures[j]
		copy(f.Values, sf.Values)
		f.Brightness, f.Enabled = sf.Brightness, sf.Enabled
	}
	c.changed()
	return nil
}

// SetMotion changes how a mood moves and reacts to music. A playing mood
// picks the change up straight away.
func (c *Controller) SetMotion(id string, mo Motion) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	mo.Speed, mo.Hold, mo.Pulse = clampF(mo.Speed, 0, 100), clampF(mo.Hold, 0, 95), clampF(mo.Pulse, 0, 100)
	c.cfg.Moods[i].Motion = mo
	c.refreshCycle(c.cfg.Moods[i])
	c.changed()
	return nil
}

// refreshCycle updates the playing cycle after its mood changed, keeping
// its place. Called with mu held.
func (c *Controller) refreshCycle(m Mood) {
	if c.playingID != m.ID {
		return
	}
	c.pulse = 0
	if len(m.Points) < 2 {
		c.cycle = nil
		c.pulse = m.Motion.Pulse
		return
	}
	if c.cycle == nil {
		c.startCycle(m, c.now())
		return
	}
	c.cycle.points, c.cycle.motion = m.Points, m.Motion
	c.cycle.phase = math.Mod(c.cycle.phase, float64(len(m.Points)))
}

// stopMotion ends a moving mood when something is changed by hand.
// Called with mu held.
func (c *Controller) stopMotion() {
	c.cycle, c.pulse, c.xfade, c.playingID, c.dark = nil, 0, nil, "", false
}

// pointColors previews each point by its first lit color fixture.
func (c *Controller) pointColors(m Mood) []string {
	out := []string{}
	for _, p := range m.Points {
		css := "rgb(0,0,0)"
		for i, sf := range p.Fixtures {
			if i < len(c.cfg.Fixtures) && sf.Enabled && c.cfg.Fixtures[i].HasColor() && len(sf.Values) == len(c.cfg.Fixtures[i].Values) {
				f := c.cfg.Fixtures[i]
				f.Values = sf.Values
				if css = previewCSS(f); css != "rgb(0,0,0)" {
					break
				}
			}
		}
		out = append(out, css)
	}
	return out
}
