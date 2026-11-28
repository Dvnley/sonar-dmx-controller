package controller

import (
	"strings"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Show looks are ready-made moods that give each kind of light its own job:
// the washes run one effect, the spider another, and the moving spots move,
// so the room looks designed rather than everything doing the same thing.

// lookPart is what one kind of light does in a show look.
type lookPart struct {
	rgb       [3]uint8 // still color (when there is no program)
	program   string
	variation int
	speed     float64
	size      float64
	level     int // brightness 0-100; 0 switches the light off
}

// spotPart is what the moving spots do.
type spotPart struct {
	pattern string // "" stays still
	speed   float64
	size    float64
	level   int
	wheel   int // color wheel value, -1 leaves it
	spin    int // pattern spin/spread value, -1 leaves it
}

type showLook struct {
	name   string
	master int
	wash   lookPart // Chauvet wash, and any other color light that is not the spider
	spider lookPart
	spots  spotPart
}

var showLooks = []showLook{
	{"Ocean floor", 85,
		lookPart{program: "ocean", speed: 25, size: 0, level: 100},
		lookPart{program: "ocean", speed: 18, size: 0, level: 80},
		spotPart{"wander", 15, 45, 60, -1, -1}},
	{"Under water", 80,
		lookPart{program: "water", speed: 22, size: 100, level: 100},
		lookPart{program: "water", speed: 18, size: 100, level: 100},
		spotPart{"wander", 10, 40, 70, 0, -1}},
	{"Thunderstorm", 100,
		lookPart{program: "lightning", speed: 55, level: 100},
		lookPart{rgb: [3]uint8{0, 0, 80}, level: 60},
		spotPart{"sweep", 70, 100, 100, -1, -1}},
	{"Sunset lounge", 70,
		lookPart{program: "sunset", speed: 15, level: 100},
		lookPart{program: "breathe", variation: 1, speed: 20, level: 70},
		spotPart{"", 0, 0, 0, -1, -1}},
	{"Heartbeat", 100,
		lookPart{program: "heartbeat", variation: 4, speed: 35, level: 100},
		lookPart{program: "heartbeat", variation: 5, speed: 35, level: 100},
		spotPart{"nod", 35, 30, 80, -1, -1}},
	{"Disco fever", 100,
		lookPart{program: "disco", speed: 65, level: 100},
		lookPart{program: "rainbow", speed: 70, level: 100},
		spotPart{"figure8", 75, 80, 100, -1, 200}},
	{"Spotlight show", 100,
		lookPart{rgb: [3]uint8{60, 0, 160}, level: 35},
		lookPart{rgb: [3]uint8{0, 0, 0}, level: 0},
		spotPart{"circle", 45, 100, 100, -1, -1}},
	{"Fire pit", 85,
		lookPart{program: "fire", speed: 50, level: 100},
		lookPart{program: "fire", speed: 40, level: 90},
		spotPart{"wander", 20, 30, 50, -1, -1}},
	{"Police raid", 100,
		lookPart{program: "police", speed: 70, level: 100},
		lookPart{program: "strobe", variation: 4, speed: 75, level: 100},
		spotPart{"sweep", 90, 100, 100, -1, -1}},
	{"Cool breeze", 75,
		lookPart{program: "breathe", variation: 2, speed: 18, level: 100},
		lookPart{program: "fade", variation: 2, speed: 12, size: 100, level: 80},
		spotPart{"circle", 12, 40, 50, -1, -1}},
	{"Pink party", 100,
		lookPart{program: "pulse", variation: 5, speed: 60, level: 100},
		lookPart{program: "jump", variation: 5, speed: 60, level: 100},
		spotPart{"wander", 55, 70, 100, -1, -1}},
}

// addShowLooks must be called with mu held. It returns how many it added.
func (c *Controller) addShowLooks() int {
	added := 0
	for _, sl := range showLooks {
		exists := false
		for _, m := range c.cfg.Moods {
			if strings.EqualFold(m.Name, sl.name) {
				exists = true
			}
		}
		if exists {
			continue
		}
		m := c.buildLook(sl)
		m.ID = c.newMoodID(sl.name)
		c.cfg.Moods = append(c.cfg.Moods, m)
		added++
	}
	return added
}

// buildLook must be called with mu held. It turns a show look into a mood
// for the current patch.
func (c *Controller) buildLook(sl showLook) Mood {
	m := Mood{Name: sl.name, Master: sl.master, Motion: Motion{Speed: 35, Hold: 20}}
	var washIdx, spiderIdx, spotIdx []int
	for i, f := range c.cfg.Fixtures {
		f.Values = append(fixture.Values(nil), f.Values...)
		var part *lookPart
		switch {
		case f.Profile == "spider-10ch":
			part = &sl.spider
			spiderIdx = append(spiderIdx, i)
		case f.HasColor() && f.Profile != UplightProfile:
			part = &sl.wash
			washIdx = append(washIdx, i)
		case moves(f):
			spotIdx = append(spotIdx, i)
			f.Enabled = sl.spots.level > 0
			f.Brightness = sl.spots.level
			if d := f.Find(fixture.Dimmer); d >= 0 && f.Values[d] == 0 {
				f.Values[d] = 255
			}
			if w := f.Find(fixture.Color); w >= 0 && sl.spots.wheel >= 0 {
				f.Values[w] = uint8(sl.spots.wheel)
			}
			if g := f.Find(fixture.Gobo); g >= 0 && sl.spots.spin >= 0 {
				f.Values[g] = uint8(sl.spots.spin)
			}
		}
		if part != nil {
			setRGB(&f, part.rgb[0], part.rgb[1], part.rgb[2])
			f.Enabled, f.Brightness = part.level > 0, part.level
		}
		m.Fixtures = append(m.Fixtures, SceneFixture{Values: f.Values, Brightness: f.Brightness, Enabled: f.Enabled})
	}
	addRun := func(idx []int, p lookPart) {
		if p.program != "" && len(idx) > 0 && p.level > 0 {
			m.Runs = append(m.Runs, ProgramRun{Fixtures: idx, Program: p.program, Speed: p.speed, Size: p.size, Variation: p.variation})
		}
	}
	addRun(washIdx, sl.wash)
	addRun(spiderIdx, sl.spider)
	if sl.spots.pattern != "" && len(spotIdx) > 0 && sl.spots.level > 0 {
		m.Moves = append(m.Moves, MoveRun{Fixtures: spotIdx, Pattern: sl.spots.pattern, Speed: sl.spots.speed, Size: sl.spots.size})
	}
	return m
}
