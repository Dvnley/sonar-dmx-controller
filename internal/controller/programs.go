package controller

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Party programs, like the built-in auto programs on Chauvet fixtures but run
// across the whole rig. Each run drives a set of color fixtures; a fixture
// belongs to at most one run. Programs move on a beat clock: either a free
// running tempo set by the speed slider, or the music beat (mic or tap).

// ProgramInfo describes a program for the UI.
type ProgramInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Programs is the catalog, in the order the UI shows it.
var Programs = []ProgramInfo{
	{"chase", "Chase"}, {"jump", "Color jump"}, {"fade", "Color fade"}, {"rainbow", "Rainbow"},
	{"wave", "Wave"}, {"pulse", "Pulse"}, {"strobe", "Strobe pop"}, {"police", "Police"},
	{"fire", "Fire"}, {"sparkle", "Sparkle"},
}

// Palettes are the program variations.
var Palettes = []struct {
	Name   string     `json:"name"`
	Colors [][3]uint8 `json:"colors"`
}{
	{"Rainbow", [][3]uint8{{255, 0, 0}, {255, 140, 0}, {255, 255, 0}, {0, 255, 0}, {0, 200, 255}, {0, 0, 255}, {160, 0, 255}}},
	{"Warm", [][3]uint8{{255, 0, 0}, {255, 60, 0}, {255, 140, 0}, {255, 200, 40}}},
	{"Cool", [][3]uint8{{0, 0, 255}, {0, 200, 255}, {120, 0, 255}, {0, 255, 120}}},
	{"Party", [][3]uint8{{255, 0, 200}, {0, 220, 255}, {255, 230, 0}, {90, 0, 255}}},
	{"Red/Blue", [][3]uint8{{255, 0, 0}, {0, 0, 255}}},
	{"Pink/Purple", [][3]uint8{{255, 40, 140}, {150, 0, 255}, {255, 0, 255}}},
}

// ProgramRun is one running program.
type ProgramRun struct {
	Fixtures  []int   `json:"fixtures"`
	Program   string  `json:"program"`
	Speed     float64 `json:"speed"`     // 0-100: 30-240 BPM free running, or a beat multiplier in music mode
	Size      float64 `json:"size"`      // 0-100: how far the pattern is spread across fixtures
	Variation int     `json:"variation"` // palette index
	Music     bool    `json:"music"`     // follow the music beat
	start     time.Time
}

// bpm is the free-running tempo for a speed setting.
func (r *ProgramRun) bpm() float64 { return 30 * math.Pow(8, clampF(r.Speed, 0, 100)/100) }

// musicRate maps the speed slider to a beat multiplier in music mode:
// 1/4, 1/2, 1, 2 or 4 steps per beat.
func (r *ProgramRun) musicRate() float64 {
	return math.Pow(2, math.Round(clampF(r.Speed, 0, 100)/25)-2)
}

// BeatClock follows the music: mic beats from the browser or taps.
type BeatClock struct {
	last   time.Time
	period time.Duration
	taps   []time.Time
	count  float64
	Source string
}

// BeatState is the music part of GET /api/state.
type BeatState struct {
	BPM    float64 `json:"bpm"`
	Source string  `json:"source"`
	Live   bool    `json:"live"` // a beat arrived in the last few seconds
}

func (b *BeatClock) beat(now time.Time, source string) {
	if source == "tap" {
		if len(b.taps) > 0 && now.Sub(b.taps[len(b.taps)-1]) > 2*time.Second {
			b.taps = nil
		}
		b.taps = append(b.taps, now)
		if len(b.taps) > 5 {
			b.taps = b.taps[len(b.taps)-5:]
		}
		if n := len(b.taps); n >= 2 {
			b.period = b.taps[n-1].Sub(b.taps[0]) / time.Duration(n-1)
		}
	} else if !b.last.IsZero() {
		gap := now.Sub(b.last)
		// fold double and half time into 70-180 BPM, then smooth
		for gap > 0 && gap < 333*time.Millisecond {
			gap *= 2
		}
		for gap > 857*time.Millisecond && gap < 3*time.Second {
			gap /= 2
		}
		if gap >= 333*time.Millisecond && gap <= 857*time.Millisecond {
			if b.period == 0 {
				b.period = gap
			} else {
				b.period = time.Duration(0.8*float64(b.period) + 0.2*float64(gap))
			}
		}
	}
	b.count = math.Floor(b.position(now)) + 1
	b.last = now
	b.Source = source
}

// position is the running beat count, extrapolated between beats.
func (b *BeatClock) position(now time.Time) float64 {
	if b.last.IsZero() || b.period == 0 {
		return b.count
	}
	return b.count + float64(now.Sub(b.last))/float64(b.period)
}

func (b *BeatClock) state(now time.Time) BeatState {
	s := BeatState{Source: b.Source, Live: !b.last.IsZero() && now.Sub(b.last) < 4*time.Second}
	if b.period > 0 {
		s.BPM = math.Round(60/b.period.Seconds()*10) / 10
	}
	return s
}

// Beat records a beat from the mic detector or a tap.
func (c *Controller) Beat(source string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if source != "tap" {
		source = "mic"
	}
	c.beats.beat(c.now(), source)
}

// StartProgram runs a program on the given color fixtures (nil = all),
// taking them out of any other run. Re-sending the same program keeps its
// phase, so dragging the speed slider does not restart it.
func (c *Controller) StartProgram(targets []int, run ProgramRun) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !validProgram(run.Program) {
		return fmt.Errorf("unknown program %q", run.Program)
	}
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	var fx []int
	for _, i := range idx {
		if c.cfg.Fixtures[i].HasColor() {
			fx = append(fx, i)
		}
	}
	if len(fx) == 0 {
		return errors.New("no color fixtures in that selection")
	}
	sort.Ints(fx)
	run.Fixtures = fx
	run.Variation = clamp(run.Variation, 0, len(Palettes)-1)
	run.start = c.now()
	for _, r := range c.runs {
		if r.Program == run.Program && sameInts(r.Fixtures, fx) {
			run.start = r.start
		}
	}
	c.removeFromRuns(fx)
	c.rainbowSec = 0
	c.runs = append(c.runs, &run)
	return nil
}

// StopProgram stops programs on the given fixtures (nil = all).
func (c *Controller) StopProgram(targets []int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if targets == nil {
		c.runs = nil
		return nil
	}
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	c.removeFromRuns(idx)
	return nil
}

// removeFromRuns must be called with mu held.
func (c *Controller) removeFromRuns(idx []int) {
	drop := map[int]bool{}
	for _, i := range idx {
		drop[i] = true
	}
	var keep []*ProgramRun
	for _, r := range c.runs {
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
	c.runs = keep
}

// applyPrograms overwrites the colors of fixtures that are in a run.
// Called with mu held, on a copy of the patch.
func (c *Controller) applyPrograms(fixtures []fixture.Fixture, now time.Time) {
	for _, r := range c.runs {
		var pos float64
		if r.Music && c.beats.period > 0 {
			pos = c.beats.position(now) * r.musicRate()
		} else {
			pos = now.Sub(r.start).Minutes() * r.bpm()
		}
		n := len(r.Fixtures)
		for k, i := range r.Fixtures {
			if i >= len(fixtures) || !fixtures[i].Enabled {
				continue
			}
			rgb, level := programColor(r, pos, k, n)
			f := fixtures[i]
			f.Values = append(fixture.Values(nil), f.Values...)
			setRGB(&f, scale8(rgb[0], level), scale8(rgb[1], level), scale8(rgb[2], level))
			fixtures[i] = f
		}
	}
}

// programColor is the heart of the programs: the color and level (0-1) of
// fixture k of n at beat position pos.
func programColor(r *ProgramRun, pos float64, k, n int) ([3]uint8, float64) {
	pal := Palettes[clamp(r.Variation, 0, len(Palettes)-1)].Colors
	spread := clampF(r.Size, 0, 100) / 100 // 0 = all fixtures together, 1 = spread across the rig
	off := float64(k) / float64(max(n, 1)) * spread
	at := func(x float64) [3]uint8 {
		m := float64(len(pal))
		x = math.Mod(math.Mod(x, m)+m, m)
		return pal[int(x)]
	}
	beat := math.Floor(pos)
	frac := pos - beat
	switch r.Program {
	case "chase":
		width := 1 + int(spread*float64(n-1)/2)
		step := int(beat) % max(n, 1)
		on := false
		for w := 0; w < width; w++ {
			if (step+w)%n == k {
				on = true
			}
		}
		if !on {
			return [3]uint8{}, 0
		}
		return at(beat / float64(max(n, 1))), 1
	case "jump":
		return at(beat + math.Round(off*float64(n))), 1
	case "fade":
		x := pos/4 + off*float64(len(pal))
		a, b := at(math.Floor(x)), at(math.Floor(x)+1)
		return mixRGB(a, b, x-math.Floor(x)), 1
	case "rainbow":
		h := math.Mod(pos/16+off, 1)
		r8, g8, b8 := hsv(h, 1, 1)
		return [3]uint8{r8, g8, b8}, 1
	case "wave":
		level := 0.5 + 0.5*math.Sin(2*math.Pi*(pos/4-off))
		return at(pos / 8), level
	case "pulse":
		return at(beat), math.Exp(-frac * 4)
	case "strobe":
		if frac < 0.12 {
			return [3]uint8{255, 255, 255}, 1
		}
		return at(beat), 0.15
	case "police":
		half := int(math.Floor(pos * 2))
		side := k < (n+1)/2
		if (half%2 == 0) == side {
			if half%2 == 0 {
				return [3]uint8{255, 0, 0}, 1
			}
			return [3]uint8{0, 0, 255}, 1
		}
		return [3]uint8{}, 0
	case "fire":
		t := math.Floor(pos * 6)
		flick := 0.55 + 0.45*noise(t, k)
		return mixRGB([3]uint8{255, 30, 0}, [3]uint8{255, 140, 0}, noise(t+7, k)), flick
	case "sparkle":
		t := math.Floor(pos * 4)
		if noise(t, k) > 0.8 {
			return [3]uint8{255, 255, 255}, 1
		}
		return at(pos/8 + off*float64(len(pal))), 0.6
	}
	return [3]uint8{}, 0
}

// noise is a cheap deterministic 0-1 hash for flicker effects.
func noise(t float64, k int) float64 {
	x := math.Sin(t*12.9898+float64(k)*78.233) * 43758.5453
	return x - math.Floor(x)
}

func mixRGB(a, b [3]uint8, t float64) [3]uint8 {
	var out [3]uint8
	for i := range out {
		out[i] = uint8(math.Round(float64(a[i]) + (float64(b[i])-float64(a[i]))*t))
	}
	return out
}

func scale8(v uint8, level float64) uint8 { return uint8(math.Round(float64(v) * clampF(level, 0, 1))) }

func clampF(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

func validProgram(id string) bool {
	for _, p := range Programs {
		if p.ID == id {
			return true
		}
	}
	return false
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SetGroup changes brightness or on/off for several fixtures at once.
func (c *Controller) SetGroup(targets []int, brightness *int, enabled *bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	for _, i := range idx {
		f := &c.cfg.Fixtures[i]
		if brightness != nil {
			f.Brightness = clamp(*brightness, 0, 100)
		}
		if enabled != nil {
			f.Enabled = *enabled
		}
	}
	c.changed()
	return nil
}

// SetPosition places a fixture on the stage map (0-1 across and down).
func (c *Controller) SetPosition(index int, x, y float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.cfg.Fixtures) {
		return fmt.Errorf("no fixture %d", index)
	}
	c.cfg.Fixtures[index].Pos = &fixture.Point{X: clampF(x, 0, 1), Y: clampF(y, 0, 1)}
	c.changed()
	return nil
}
