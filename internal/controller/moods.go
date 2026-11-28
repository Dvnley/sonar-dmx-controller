package controller

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"

	// The iPad has no zoneinfo files Go can read, so the schedule carries
	// its own copy of the time zone database.
	_ "time/tzdata"
)

// Moods and the schedule: a mood is a whole saved look (colors, levels,
// which programs run and how fast, master) and the schedule plays moods at
// clock times so the rig can run a night on its own. The schedule is checked
// every second in the output loop, and on launch it goes straight to the mood
// that should be playing, so a restart or power cut picks up where it was.

// Mood is a saved look the schedule can play.
type Mood struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Fixtures []SceneFixture `json:"fixtures"`
	Runs     []ProgramRun   `json:"runs,omitempty"`
	Moves    []MoveRun      `json:"moves,omitempty"` // movement of the moving lights
	Master   int            `json:"master"`
	Rainbow  float64        `json:"rainbow_s,omitempty"`
	// Points make it a moving mood: it glides from one to the next.
	Points []MoodPoint `json:"points,omitempty"`
	Motion Motion      `json:"motion"`
}

// MoodOff is the schedule's "lights off" entry.
const MoodOff = "off"

// ScheduleEntry plays a mood at a clock time.
type ScheduleEntry struct {
	Time string  `json:"time"` // "21:30", 24-hour, in the schedule's time zone
	Days []int   `json:"days"` // 0 = Sunday ... 6 = Saturday; empty = every day
	Mood string  `json:"mood"` // a mood ID, or "off"
	Fade float64 `json:"fade"` // crossfade seconds
}

// Schedule is saved in the config.
type Schedule struct {
	Enabled  bool            `json:"enabled"`
	TimeZone string          `json:"time_zone,omitempty"` // IANA name from the browser, e.g. America/New_York
	Entries  []ScheduleEntry `json:"entries"`
}

// MaxMoodFade is the longest crossfade between moods.
const MaxMoodFade = 30 * time.Minute

// crossfade blends the whole output from a frozen look to the live one, so
// programs fade into each other too.
type crossfade struct {
	from  []fixture.Fixture
	start time.Time
	dur   time.Duration
}

// MoodInfo describes a mood for the UI.
type MoodInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Colors  []string `json:"colors"`
	Program string   `json:"program,omitempty"`
	Speed   float64  `json:"speed"`
	Music   bool     `json:"music,omitempty"`
	Points  []string `json:"points"` // a preview color per marked point
	Motion  Motion   `json:"motion"`
	MoveTxt string   `json:"motion_label"`
}

// Occurrence is one time an entry plays.
type Occurrence struct {
	Entry int       `json:"entry"`
	Mood  string    `json:"mood"`
	Name  string    `json:"name"`
	At    time.Time `json:"at"`
	Clock string    `json:"clock"` // e.g. "Fri 9:00 PM"
}

// ScheduleState is the schedule part of GET /api/state.
type ScheduleState struct {
	Schedule
	Now      string      `json:"now"`
	Zone     string      `json:"zone"`
	Current  *Occurrence `json:"current"`
	Next     *Occurrence `json:"next"`
	Playing  string      `json:"playing"` // name of the last mood played, by hand or by the schedule
	ByHand   bool        `json:"by_hand"` // the last mood was picked by hand, not by the schedule
	Problems []string    `json:"problems"`
}

func (c *Controller) findMood(id string) int {
	for i, m := range c.cfg.Moods {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func (c *Controller) newMoodID(name string) string {
	base := "mood-" + fixture.Slug(name)
	if base == "mood-" {
		base = "mood"
	}
	id := base
	for n := 2; c.findMood(id) >= 0 || id == MoodOff; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// captureMood must be called with mu held.
func (c *Controller) captureMood() Mood {
	m := Mood{Master: c.cfg.Master, Rainbow: c.rainbowSec}
	for _, f := range c.cfg.Fixtures {
		m.Fixtures = append(m.Fixtures, SceneFixture{
			Values: append(fixture.Values(nil), f.Values...), Brightness: f.Brightness, Enabled: f.Enabled,
		})
	}
	for _, r := range c.runs {
		run := *r
		run.Fixtures = append([]int(nil), r.Fixtures...)
		m.Runs = append(m.Runs, run)
	}
	for _, r := range c.moves {
		run := *r
		run.Fixtures = append([]int(nil), r.Fixtures...)
		m.Moves = append(m.Moves, run)
	}
	return m
}

// SaveMood stores the current look as a mood. With an ID it overwrites that
// mood (keeping its name unless a new one is given), else it adds a new one.
func (c *Controller) SaveMood(id, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name = strings.TrimSpace(name)
	m := c.captureMood()
	if id != "" {
		i := c.findMood(id)
		if i < 0 {
			return "", fmt.Errorf("no mood %q", id)
		}
		old := c.cfg.Moods[i]
		m.ID, m.Name, m.Points, m.Motion = id, old.Name, old.Points, old.Motion
		if name != "" {
			m.Name = name
		}
		c.cfg.Moods[i] = m
	} else {
		if name == "" {
			name = fmt.Sprintf("Mood %d", len(c.cfg.Moods)+1)
		}
		m.ID, m.Name = c.newMoodID(name), name
		c.cfg.Moods = append(c.cfg.Moods, m)
	}
	c.changed()
	return m.ID, nil
}

// RenameMood changes a mood's name.
func (c *Controller) RenameMood(id, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	if name = strings.TrimSpace(name); name == "" {
		return errors.New("give the mood a name")
	}
	c.cfg.Moods[i].Name = name
	c.changed()
	return nil
}

// DeleteMood removes a mood and the schedule entries that play it.
func (c *Controller) DeleteMood(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	c.cfg.Moods = append(c.cfg.Moods[:i], c.cfg.Moods[i+1:]...)
	if c.playingID == id {
		c.stopMotion()
	}
	var keep []ScheduleEntry
	for _, e := range c.cfg.Schedule.Entries {
		if e.Mood != id {
			keep = append(keep, e)
		}
	}
	c.cfg.Schedule.Entries = keep
	c.changed()
	return nil
}

// MoveMood moves a mood up (-1) or down (+1) in the list.
func (c *Controller) MoveMood(id string, delta int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.findMood(id)
	if i < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	j := clamp(i+delta, 0, len(c.cfg.Moods)-1)
	c.cfg.Moods[i], c.cfg.Moods[j] = c.cfg.Moods[j], c.cfg.Moods[i]
	c.changed()
	return nil
}

// PlayMood crossfades to a mood by hand. The schedule, if on, still takes
// over at its next time.
func (c *Controller) PlayMood(id string, fadeSec float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id != MoodOff && c.findMood(id) < 0 {
		return fmt.Errorf("no mood %q", id)
	}
	c.applyMood(id, fadeSec)
	c.moodByHand = true
	return nil
}

// applyMood must be called with mu held.
func (c *Controller) applyMood(id string, fadeSec float64) {
	now := c.now()
	if dur := time.Duration(math.Min(math.Max(fadeSec, 0), MaxMoodFade.Seconds()) * float64(time.Second)); dur > 0 && len(c.live) == len(c.cfg.Fixtures) {
		from := make([]fixture.Fixture, len(c.live))
		for i, f := range c.live {
			f.Values = append(fixture.Values(nil), f.Values...)
			from[i] = f
		}
		c.xfade = &crossfade{from: from, start: now, dur: dur}
	} else {
		c.xfade = nil
	}
	c.fades = map[int]*fade{}
	c.flashUntil, c.strobeHz, c.blackout = time.Time{}, 0, false
	c.runs, c.rainbowSec, c.moves = nil, 0, nil
	c.cycle, c.pulse, c.playingID, c.dark = nil, 0, id, false
	if id == MoodOff {
		// fade to dark, then hold a blackout (the Blackout button undoes it)
		c.dark = true
		c.playing = "Lights off"
		c.changed()
		return
	}
	m := c.cfg.Moods[c.findMood(id)]
	for i, sf := range m.Fixtures {
		if i >= len(c.cfg.Fixtures) {
			break
		}
		f := &c.cfg.Fixtures[i]
		if len(sf.Values) != len(f.Values) {
			continue // the patch changed since the mood was saved
		}
		copy(f.Values, sf.Values)
		f.Brightness, f.Enabled = sf.Brightness, sf.Enabled
	}
	if len(m.Points) >= 2 {
		c.startCycle(m, now)
	} else {
		c.pulse = m.Motion.Pulse
	}
	for _, r := range m.Runs {
		if c.cycle != nil {
			break // the points replace the programs
		}
		run := r
		run.Fixtures = nil
		for _, i := range r.Fixtures {
			if i >= 0 && i < len(c.cfg.Fixtures) && c.cfg.Fixtures[i].HasColor() {
				run.Fixtures = append(run.Fixtures, i)
			}
		}
		if len(run.Fixtures) == 0 || !validProgram(run.Program) {
			continue
		}
		run.Variation = clamp(run.Variation, 0, len(Palettes)-1)
		run.start = now
		c.removeFromRuns(run.Fixtures)
		c.runs = append(c.runs, &run)
	}
	for _, r := range m.Moves {
		run := r
		run.Fixtures = nil
		for _, i := range r.Fixtures {
			if i >= 0 && i < len(c.cfg.Fixtures) && moves(c.cfg.Fixtures[i]) {
				run.Fixtures = append(run.Fixtures, i)
			}
		}
		if len(run.Fixtures) == 0 || !validMovement(run.Pattern) {
			continue
		}
		run.last = now
		c.moves = append(c.moves, &run)
	}
	if m.Rainbow > 0 && c.cycle == nil {
		c.rainbowSec, c.rainbowStart = m.Rainbow, now
	}
	c.cfg.Master = clamp(m.Master, 0, 100)
	c.playing = m.Name
	c.changed()
}

// applyCrossfade blends the frame toward the live look. Called with mu held.
func (c *Controller) applyCrossfade(fixtures []fixture.Fixture, now time.Time) {
	xf := c.xfade
	if xf == nil {
		return
	}
	t := float64(now.Sub(xf.start)) / float64(xf.dur)
	if t >= 1 || len(xf.from) != len(fixtures) {
		c.xfade = nil
		return
	}
	t = t * t * (3 - 2*t) // ease in and out
	for i := range fixtures {
		from, to := xf.from[i], fixtures[i]
		if len(from.Values) != len(to.Values) {
			continue
		}
		level := func(f fixture.Fixture) float64 {
			if !f.Enabled {
				return 0
			}
			return float64(f.Brightness)
		}
		vals := make(fixture.Values, len(to.Values))
		for j := range vals {
			a, b := float64(from.Values[j]), float64(to.Values[j])
			// a light that is fading in or out keeps its other color instead of fading through black
			if !from.Enabled {
				a = b
			} else if !to.Enabled {
				b = a
			}
			vals[j] = uint8(math.Round(a + (b-a)*t))
		}
		to.Values = vals
		to.Brightness = int(math.Round(level(from) + (level(to)-level(from))*t))
		to.Enabled = to.Brightness > 0
		fixtures[i] = to
	}
}

// ---------- starter moods ----------

type starter struct {
	name      string
	rgb       [3]uint8
	program   string
	speed     float64
	size      float64
	variation int
	master    int
	pulse     float64
	points    [][3]uint8 // a moving mood: glide through these colors
}

var starters = []starter{
	{"Warm welcome", [3]uint8{255, 110, 20}, "", 0, 0, 0, 80, 0, nil},
	{"Chill blue", [3]uint8{0, 60, 255}, "fade", 12, 70, 2, 70, 0, nil},
	{"Dinner glow", [3]uint8{255, 60, 0}, "wave", 8, 90, 1, 60, 0, nil},
	{"Party", [3]uint8{255, 0, 200}, "jump", 55, 100, 3, 100, 35, nil},
	{"Peak hour", [3]uint8{255, 255, 255}, "chase", 78, 40, 0, 100, 50, nil},
	{"Last call", [3]uint8{255, 40, 140}, "fade", 20, 60, 5, 55, 0, nil},
	{"Sunset drift", [3]uint8{255, 120, 0}, "", 0, 0, 0, 75, 0, [][3]uint8{{255, 120, 0}, {255, 30, 0}, {255, 0, 120}, {120, 0, 255}}},
}

// AddStarterMoods adds a ready-made set of moods built for the current patch.
// Moods with the same name are left alone.
func (c *Controller) AddStarterMoods() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	var color []int
	for i, f := range c.cfg.Fixtures {
		if f.HasColor() {
			color = append(color, i)
		}
	}
	added := 0
	for _, s := range starters {
		exists := false
		for _, m := range c.cfg.Moods {
			if strings.EqualFold(m.Name, s.name) {
				exists = true
			}
		}
		if exists {
			continue
		}
		m := Mood{ID: c.newMoodID(s.name), Name: s.name, Master: s.master, Motion: Motion{Speed: 35, Hold: 20, Pulse: s.pulse}}
		look := func(rgb [3]uint8) []SceneFixture {
			var out []SceneFixture
			for _, f := range c.cfg.Fixtures {
				f.Values = append(fixture.Values(nil), f.Values...)
				if f.HasColor() {
					setRGB(&f, rgb[0], rgb[1], rgb[2])
					f.Brightness, f.Enabled = 100, true
				}
				out = append(out, SceneFixture{Values: f.Values, Brightness: f.Brightness, Enabled: f.Enabled})
			}
			return out
		}
		m.Fixtures = look(s.rgb)
		for _, rgb := range s.points {
			m.Points = append(m.Points, MoodPoint{Fixtures: look(rgb)})
		}
		if s.program != "" && len(color) > 0 {
			m.Runs = []ProgramRun{{Fixtures: append([]int(nil), color...), Program: s.program, Speed: s.speed, Size: s.size, Variation: s.variation}}
		}
		c.cfg.Moods = append(c.cfg.Moods, m)
		added++
	}
	added += c.addShowLooks()
	if added > 0 {
		c.changed()
	}
	return added
}

// ---------- schedule ----------

func parseClock(s string) (int, int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// SetSchedule replaces the schedule.
func (c *Controller) SetSchedule(s Schedule) error {
	if s.TimeZone != "" {
		if _, err := time.LoadLocation(s.TimeZone); err != nil {
			return fmt.Errorf("unknown time zone %q", s.TimeZone)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range s.Entries {
		e := &s.Entries[i]
		hh, mm, ok := parseClock(e.Time)
		if !ok {
			return fmt.Errorf("line %d: %q is not a time like 21:30", i+1, e.Time)
		}
		e.Time = fmt.Sprintf("%02d:%02d", hh, mm)
		if e.Mood != MoodOff && c.findMood(e.Mood) < 0 {
			return fmt.Errorf("line %d: pick a mood", i+1)
		}
		days := map[int]bool{}
		for _, d := range e.Days {
			if d < 0 || d > 6 {
				return fmt.Errorf("line %d: day %d is not 0-6", i+1, d)
			}
			days[d] = true
		}
		e.Days = e.Days[:0]
		for d := 0; d < 7; d++ {
			if days[d] {
				e.Days = append(e.Days, d)
			}
		}
		if len(e.Days) == 7 {
			e.Days = nil
		}
		e.Fade = math.Max(0, math.Min(e.Fade, MaxMoodFade.Seconds()))
	}
	sort.SliceStable(s.Entries, func(a, b int) bool { return s.Entries[a].Time < s.Entries[b].Time })
	if s.Enabled && !c.cfg.Schedule.Enabled {
		c.schedKey = "" // turning it on goes to the mood that should be playing now
	}
	c.cfg.Schedule = s
	c.changed()
	return nil
}

func (c *Controller) schedLocation() *time.Location {
	if tz := c.cfg.Schedule.TimeZone; tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

func (e ScheduleEntry) onDay(wd time.Weekday) bool {
	if len(e.Days) == 0 {
		return true
	}
	for _, d := range e.Days {
		if d == int(wd) {
			return true
		}
	}
	return false
}

// occurrence finds the latest entry time at or before now (forward=false), or
// the first one after now (forward=true), within a week. Called with mu held.
func (c *Controller) occurrence(now time.Time, forward bool) *Occurrence {
	loc := c.schedLocation()
	local := now.In(loc)
	var best *Occurrence
	for d := -8; d <= 8; d++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+d, 12, 0, 0, 0, loc)
		for i, e := range c.cfg.Schedule.Entries {
			hh, mm, ok := parseClock(e.Time)
			if !ok || !e.onDay(day.Weekday()) || (e.Mood != MoodOff && c.findMood(e.Mood) < 0) {
				continue
			}
			at := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
			if forward {
				if !at.After(now) || (best != nil && !at.Before(best.At)) {
					continue
				}
			} else if at.After(now) || (best != nil && !at.After(best.At)) {
				continue
			}
			best = &Occurrence{Entry: i, Mood: e.Mood, At: at, Clock: at.Format("Mon 3:04 PM")}
		}
	}
	if best != nil {
		best.Name = "Lights off"
		if i := c.findMood(best.Mood); i >= 0 {
			best.Name = c.cfg.Moods[i].Name
		}
	}
	return best
}

func occKey(o *Occurrence, e ScheduleEntry) string {
	return fmt.Sprintf("%d|%s|%s", o.At.Unix(), e.Time, e.Mood)
}

// CheckSchedule plays the scheduled mood when its time comes. The output loop
// calls it every second; on the first call after launch it resumes the mood
// that should be playing, without a fade.
func (c *Controller) CheckSchedule() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.cfg.Schedule.Enabled || c.sweep != nil {
		return
	}
	o := c.occurrence(c.now(), false)
	if o == nil {
		return
	}
	e := c.cfg.Schedule.Entries[o.Entry]
	key := occKey(o, e)
	if key == c.schedKey {
		return
	}
	fadeSec := e.Fade
	if c.schedKey == "" && !c.started {
		fadeSec = 0 // just launched: go straight to it
	}
	c.schedKey, c.started = key, true
	c.applyMood(e.Mood, fadeSec)
	c.moodByHand = false
}

// scheduleState must be called with mu held.
func (c *Controller) scheduleState() ScheduleState {
	now := c.now()
	loc := c.schedLocation()
	s := ScheduleState{Schedule: c.cfg.Schedule, Now: now.In(loc).Format("Mon 3:04 PM"), Zone: loc.String(),
		Playing: c.playing, ByHand: c.moodByHand, Problems: []string{}}
	if s.Entries == nil {
		s.Entries = []ScheduleEntry{}
	}
	s.Current = c.occurrence(now, false)
	s.Next = c.occurrence(now, true)
	if s.Enabled && len(s.Entries) == 0 {
		s.Problems = append(s.Problems, "The schedule is on but has no times yet.")
	}
	return s
}

// moodInfos must be called with mu held.
func (c *Controller) moodInfos() []MoodInfo {
	out := []MoodInfo{}
	for _, m := range c.cfg.Moods {
		info := MoodInfo{ID: m.ID, Name: m.Name, Colors: []string{}, Points: c.pointColors(m), Motion: m.Motion, MoveTxt: m.Motion.MotionLabel()}
		seen := map[string]bool{}
		add := func(css string) {
			if !seen[css] && len(info.Colors) < 6 && css != "rgb(0,0,0)" {
				seen[css] = true
				info.Colors = append(info.Colors, css)
			}
		}
		if len(m.Points) >= 2 {
			info.Program = fmt.Sprintf("Moves through %d points", len(m.Points))
			for _, css := range info.Points {
				add(css)
			}
		} else if len(m.Runs) > 0 {
			r := m.Runs[0]
			info.Speed, info.Music = r.Speed, r.Music
			for _, p := range Programs {
				if p.ID == r.Program {
					info.Program = p.Name
				}
			}
			switch r.Program {
			case "rainbow", "disco":
				for _, h := range []float64{0, 0.17, 0.33, 0.5, 0.67, 0.83} {
					cr, cg, cb := hsv(h, 1, 1)
					add(fmt.Sprintf("rgb(%d,%d,%d)", cr, cg, cb))
				}
			case "police":
				add("rgb(255,0,0)")
				add("rgb(0,0,255)")
			case "fire":
				add("rgb(255,30,0)")
				add("rgb(255,140,0)")
			case "ocean":
				add("rgb(0,30,255)")
				add("rgb(0,220,180)")
			case "sunset":
				add("rgb(255,140,0)")
				add("rgb(255,0,60)")
				add("rgb(170,0,200)")
			case "lightning":
				add("rgb(10,0,90)")
				add("rgb(255,255,255)")
			default:
				for _, p := range Palettes[clamp(r.Variation, 0, len(Palettes)-1)].Colors {
					add(fmt.Sprintf("rgb(%d,%d,%d)", p[0], p[1], p[2]))
				}
			}
		} else if m.Rainbow > 0 {
			info.Program = "Rainbow"
			for _, h := range []float64{0, 0.33, 0.67} {
				cr, cg, cb := hsv(h, 1, 1)
				add(fmt.Sprintf("rgb(%d,%d,%d)", cr, cg, cb))
			}
		}
		if len(info.Colors) == 0 {
			for i, sf := range m.Fixtures {
				if i < len(c.cfg.Fixtures) && sf.Enabled && c.cfg.Fixtures[i].HasColor() && len(sf.Values) == len(c.cfg.Fixtures[i].Values) {
					f := c.cfg.Fixtures[i]
					f.Values = sf.Values
					add(previewCSS(f))
				}
			}
		}
		out = append(out, info)
	}
	return out
}
