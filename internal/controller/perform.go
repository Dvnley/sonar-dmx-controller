package controller

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Live performance features: color fades, flash, strobe, a rainbow effect
// and scene memories. They all run in the output loop, so timing does not
// depend on how fast the iPad or browser can send requests.

// SceneSlots is how many scene buttons the Perform tab shows.
const SceneSlots = 8

const flashTimeout = 30 * time.Second // safety release if the button-up is lost

type fade struct {
	from  fixture.Values
	start time.Time
	dur   time.Duration
}

// Scene is a saved look: every fixture's values, brightness and on/off.
type Scene struct {
	Name     string         `json:"name"`
	Fixtures []SceneFixture `json:"fixtures"`
}

// SceneFixture is one fixture's part of a scene, matched by patch position.
type SceneFixture struct {
	Values     fixture.Values `json:"values"`
	Brightness int            `json:"brightness"`
	Enabled    bool           `json:"enabled"`
}

// PerformState is the perform part of GET /api/state.
type PerformState struct {
	Flash      bool        `json:"flash"`
	StrobeHz   float64     `json:"strobe_hz"`
	RainbowSec float64     `json:"rainbow_s"`
	Scenes     []SceneInfo `json:"scenes"`
}

// SceneInfo describes a scene button.
type SceneInfo struct {
	Slot  int    `json:"slot"`
	Name  string `json:"name"`
	Saved bool   `json:"saved"`
	Color string `json:"color,omitempty"` // CSS preview of the first color fixture
}

// beginFade must be called with mu held, before a fixture's values change.
func (c *Controller) beginFade(i int, dur time.Duration, now time.Time) {
	if dur <= 0 {
		delete(c.fades, i)
		return
	}
	from := c.shownValues(i, now)
	c.fades[i] = &fade{from: from, start: now, dur: dur}
}

// shownValues is what a fixture is outputting right now, mid-fade included.
func (c *Controller) shownValues(i int, now time.Time) fixture.Values {
	target := c.cfg.Fixtures[i].Values
	fd, ok := c.fades[i]
	if !ok || len(fd.from) != len(target) {
		return append(fixture.Values(nil), target...)
	}
	t := float64(now.Sub(fd.start)) / float64(fd.dur)
	if t >= 1 {
		return append(fixture.Values(nil), target...)
	}
	out := make(fixture.Values, len(target))
	for j := range target {
		out[j] = uint8(math.Round(float64(fd.from[j]) + (float64(target[j])-float64(fd.from[j]))*t))
	}
	return out
}

// performLook applies fades, rainbow and flash to a copy of the patch and
// reports whether the strobe is in its dark half. Called with mu held.
func (c *Controller) performLook(now time.Time) ([]fixture.Fixture, fixture.Output) {
	out := fixture.Output{Master: c.cfg.Master, Blackout: c.blackout}
	fixtures := make([]fixture.Fixture, len(c.cfg.Fixtures))
	copy(fixtures, c.cfg.Fixtures)
	for i, fd := range c.fades {
		if i >= len(fixtures) || now.Sub(fd.start) >= fd.dur {
			delete(c.fades, i)
			continue
		}
		fixtures[i].Values = c.shownValues(i, now)
	}
	if c.rainbowSec > 0 {
		var colorIdx []int
		for i, f := range fixtures {
			if f.Enabled && f.HasColor() {
				colorIdx = append(colorIdx, i)
			}
		}
		base := now.Sub(c.rainbowStart).Seconds() / c.rainbowSec
		for k, i := range colorIdx {
			hue := math.Mod(base+float64(k)/float64(len(colorIdx)), 1)
			r, g, b := hsv(hue, 1, 1)
			f := fixtures[i]
			f.Values = append(fixture.Values(nil), f.Values...)
			setRGB(&f, r, g, b)
			fixtures[i] = f
		}
	}
	if !c.flashUntil.IsZero() {
		if now.Before(c.flashUntil) {
			fixtures = testLook(fixtures, "white")
			return fixtures, fixture.Output{Master: 100}
		}
		c.flashUntil = time.Time{}
	}
	if c.strobeHz > 0 {
		phase := int(now.Sub(c.strobeStart).Seconds()*c.strobeHz*2) % 2
		if phase == 1 {
			out.Blackout = true
		}
	}
	return fixtures, out
}

// setRGB sets a pure RGB color and zeroes the other emitters.
func setRGB(f *fixture.Fixture, r, g, b uint8) {
	for j, ch := range f.Channels {
		switch ch.Type {
		case fixture.Red:
			f.Values[j] = r
		case fixture.Green:
			f.Values[j] = g
		case fixture.Blue:
			f.Values[j] = b
		case fixture.Amber, fixture.White, fixture.UV:
			f.Values[j] = 0
		}
	}
	f.Set(fixture.Dimmer, 255)
}

// hsv converts hue/saturation/value in 0..1 to RGB.
func hsv(h, s, v float64) (uint8, uint8, uint8) {
	i := math.Floor(h * 6)
	f := h*6 - i
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	var r, g, b float64
	switch int(i) % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	default:
		r, g, b = v, p, q
	}
	return uint8(math.Round(r * 255)), uint8(math.Round(g * 255)), uint8(math.Round(b * 255))
}

// SetFlash holds every color fixture at full white while on.
func (c *Controller) SetFlash(on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		c.flashUntil = c.now().Add(flashTimeout)
	} else {
		c.flashUntil = time.Time{}
	}
}

// SetStrobe flashes the output on and off at hz (0 stops it).
func (c *Controller) SetStrobe(hz float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strobeHz = math.Max(0, math.Min(hz, 20))
	c.strobeStart = c.now()
}

// SetRainbow cycles the color fixtures through the hues, one full cycle
// every seconds (0 stops it and returns to the last color).
func (c *Controller) SetRainbow(seconds float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if seconds > 0 && c.rainbowSec == 0 {
		c.rainbowStart = c.now()
	}
	c.rainbowSec = math.Max(0, math.Min(seconds, 600))
}

// SaveScene stores the current look in a slot.
func (c *Controller) SaveScene(slot int, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot < 0 || slot >= SceneSlots {
		return fmt.Errorf("scene slot must be 0-%d", SceneSlots-1)
	}
	for len(c.cfg.Scenes) < SceneSlots {
		c.cfg.Scenes = append(c.cfg.Scenes, Scene{})
	}
	sc := Scene{Name: name}
	if sc.Name == "" {
		sc.Name = fmt.Sprintf("Scene %d", slot+1)
	}
	for _, f := range c.cfg.Fixtures {
		sc.Fixtures = append(sc.Fixtures, SceneFixture{
			Values: append(fixture.Values(nil), f.Values...), Brightness: f.Brightness, Enabled: f.Enabled,
		})
	}
	c.cfg.Scenes[slot] = sc
	c.changed()
	return nil
}

// ClearScene empties a slot.
func (c *Controller) ClearScene(slot int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot < 0 || slot >= len(c.cfg.Scenes) {
		return nil
	}
	c.cfg.Scenes[slot] = Scene{}
	c.changed()
	return nil
}

// RecallScene fades to a saved look. Fixtures whose channel count changed
// since the scene was saved are left alone.
func (c *Controller) RecallScene(slot int, fadeSec float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if slot < 0 || slot >= len(c.cfg.Scenes) || len(c.cfg.Scenes[slot].Fixtures) == 0 {
		return errors.New("that scene is empty")
	}
	now := c.now()
	c.rainbowSec = 0
	for i, sf := range c.cfg.Scenes[slot].Fixtures {
		if i >= len(c.cfg.Fixtures) {
			break
		}
		f := &c.cfg.Fixtures[i]
		if len(sf.Values) != len(f.Values) {
			continue
		}
		c.beginFade(i, seconds(fadeSec), now)
		copy(f.Values, sf.Values)
		f.Brightness, f.Enabled = sf.Brightness, sf.Enabled
	}
	c.changed()
	return nil
}

func (c *Controller) performState() PerformState {
	ps := PerformState{Flash: !c.flashUntil.IsZero(), StrobeHz: c.strobeHz, RainbowSec: c.rainbowSec}
	for slot := 0; slot < SceneSlots; slot++ {
		info := SceneInfo{Slot: slot, Name: fmt.Sprintf("Scene %d", slot+1)}
		if slot < len(c.cfg.Scenes) && len(c.cfg.Scenes[slot].Fixtures) > 0 {
			sc := c.cfg.Scenes[slot]
			info.Name, info.Saved = sc.Name, true
			for i, sf := range sc.Fixtures {
				if i < len(c.cfg.Fixtures) && sf.Enabled && c.cfg.Fixtures[i].HasColor() && len(sf.Values) == len(c.cfg.Fixtures[i].Values) {
					f := c.cfg.Fixtures[i]
					f.Values = sf.Values
					info.Color = previewCSS(f)
					break
				}
			}
		}
		ps.Scenes = append(ps.Scenes, info)
	}
	return ps
}

// previewCSS approximates a fixture's color for the UI.
func previewCSS(f fixture.Fixture) string {
	v := func(t fixture.ChannelType) float64 {
		if i := f.Find(t); i >= 0 {
			return float64(f.Values[i])
		}
		return 0
	}
	w, a, uv := v(fixture.White), v(fixture.Amber), v(fixture.UV)
	r := math.Min(255, v(fixture.Red)+w+a+uv*0.4)
	g := math.Min(255, v(fixture.Green)+w+a*0.6)
	b := math.Min(255, v(fixture.Blue)+w+uv)
	return fmt.Sprintf("rgb(%d,%d,%d)", int(r), int(g), int(b))
}

func seconds(s float64) time.Duration {
	if s <= 0 {
		return 0
	}
	return time.Duration(math.Min(s, 60) * float64(time.Second))
}
