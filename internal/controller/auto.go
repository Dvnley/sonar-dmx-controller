package controller

import (
	"math"
	"math/rand"
	"time"
)

// Auto mode runs the show by itself. The browser on the Mac listens with the
// mic and sends how loud the room is a few times a second (and the kick drum
// beats, as the music mode does). From that the controller works out the
// energy of the music: quiet parts get slow, calm looks and loud parts get
// fast, busy ones. It changes look every phrase, jumps straight to a big look
// when the music drops, and hands over to the spots alone (a spotlight
// moment) when a loud part suddenly goes quiet. With no mic it still changes
// looks by itself at the Speed slider's pace.

// The looks Auto picks from, by energy.
var autoTiers = [3][]string{
	{"Ocean floor", "Sunset lounge", "Cool breeze", "Fire pit", "Under water"},
	{"Heartbeat", "Pink party", "Fire pit", "Under water", "Spotlight show"},
	{"Disco fever", "Thunderstorm", "Police raid", "Pink party"},
}

// The spot moves Auto picks from, by energy.
var autoMoves = [3][]string{
	{"wander", "circle"},
	{"circle", "nod", "figure8", "wander"},
	{"sweep", "figure8", "circle"},
}

var autoTierNames = [3]string{"Calm", "Groove", "Peak"}

type autoState struct {
	on          bool
	sensitivity int // 0-100, 50 is normal

	heard  time.Time // last loudness report
	fast   float64   // loudness, smoothed over about a second
	slow   float64   // loudness, smoothed over about ten seconds
	peak   float64   // loudest recent level, slowly forgotten
	energy float64   // 0-1, what the show follows

	tier       int
	tierSince  time.Time // when the energy moved into another tier
	wantTier   int
	look       string
	moment     string // "drop" or "spotlight" just after one
	changed    time.Time
	beatAt     float64 // beat count at the last change
	highSince  time.Time
	lastSpeed  float64
	lastMusic  bool
	lastUpdate time.Time
	rng        *rand.Rand
}

// AutoState is the auto part of GET /api/state.
type AutoState struct {
	On          bool   `json:"on"`
	Sensitivity int    `json:"sensitivity"`
	Listening   bool   `json:"listening"`
	Energy      int    `json:"energy"` // 0-100
	Level       string `json:"level"`  // Calm, Groove, Peak
	Look        string `json:"look"`
	Moment      string `json:"moment,omitempty"`
}

// SetAuto turns Auto on or off and sets how sensitive it is to the mic.
func (c *Controller) SetAuto(on *bool, sensitivity *int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := &c.auto
	if sensitivity != nil {
		a.sensitivity = clamp(*sensitivity, 0, 100)
	}
	if on == nil || *on == a.on {
		return
	}
	a.on = *on
	if !a.on {
		return
	}
	now := c.now()
	if a.rng == nil {
		a.rng = rand.New(rand.NewSource(now.UnixNano()))
	}
	if a.energy == 0 {
		a.energy = 0.5
	}
	a.tier = tierOf(a.energy)
	a.wantTier, a.tierSince = a.tier, now
	c.blackout, c.dark = false, false
	c.autoChange(now, "", 1.5)
}

// AutoLevel takes a loudness report (the mic's RMS level, 0-1).
func (c *Controller) AutoLevel(level float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := &c.auto
	now := c.now()
	level = clampF(level, 0, 1)
	dt := 0.25
	if !a.heard.IsZero() {
		dt = clampF(now.Sub(a.heard).Seconds(), 0.01, 2)
	}
	if a.heard.IsZero() || now.Sub(a.heard) > 5*time.Second {
		a.fast, a.slow, a.peak = level, level, level
	}
	a.heard = now
	a.fast += (level - a.fast) * (1 - math.Exp(-dt/0.8))
	a.slow += (level - a.slow) * (1 - math.Exp(-dt/10))
	// the peak follows the loudest moments and forgets over about a minute,
	// so Auto adjusts to how loud the room is
	a.peak = math.Max(a.fast, a.peak*math.Exp(-dt/60))
}

// autoListening must be called with mu held.
func (c *Controller) autoListening(now time.Time) bool {
	return !c.auto.heard.IsZero() && now.Sub(c.auto.heard) < 3*time.Second
}

func tierOf(e float64) int {
	switch {
	case e < 0.35:
		return 0
	case e < 0.7:
		return 1
	}
	return 2
}

// autoTick runs Auto. The output loop calls it a few times a second.
func (c *Controller) autoTick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := &c.auto
	if !a.on || c.sweep != nil {
		return
	}
	now := c.now()
	dt := 0.25
	if !a.lastUpdate.IsZero() {
		dt = clampF(now.Sub(a.lastUpdate).Seconds(), 0.01, 2)
	}
	a.lastUpdate = now

	// work out the energy
	target := c.showSpeed / 100 // no mic: the Speed slider sets the mood
	if !c.showSpeedSet {
		target = 0.5
	}
	listening := c.autoListening(now)
	var jump float64
	if listening {
		// Loudness in decibels below the recent peak: about 15 dB of range at
		// normal sensitivity, so a verse sits in the middle and the chorus at
		// the top. Higher sensitivity widens the range (quieter counts more).
		span := 8 + 0.14*float64(a.sensitivity)
		quiet := 0.0015 * math.Pow(2, (50-float64(a.sensitivity))/25) // room silence
		db := func(v float64) float64 { return 20 * math.Log10(math.Max(v, 1e-6)) }
		if a.peak < quiet || a.fast < quiet {
			target = 0
		} else {
			target = clampF(1-(db(a.peak)-db(a.fast))/span, 0, 1)
		}
		jump = (db(a.fast) - db(math.Max(a.slow, quiet))) / span
	}
	a.energy += (target - a.energy) * (1 - math.Exp(-dt/1.5))

	// lights off and blackout win over Auto
	if c.blackout || c.dark {
		return
	}

	// the drop: the music gets much louder all at once
	sinceChange := now.Sub(a.changed)
	if listening && jump > 0.35 && target > 0.7 && sinceChange > 4*time.Second {
		a.energy = math.Max(a.energy, target)
		a.tier, a.wantTier, a.tierSince = 2, 2, now
		c.autoChange(now, "drop", 0)
		return
	}
	// the break: a loud part goes quiet, so the spots get the room
	if a.tier == 2 {
		if a.highSince.IsZero() {
			a.highSince = now
		}
	} else if a.tier < 1 || target > 0.5 {
		a.highSince = time.Time{}
	}
	if listening && !a.highSince.IsZero() && now.Sub(a.highSince) > 8*time.Second && target < 0.25 && jump < -0.25 && sinceChange > 4*time.Second {
		a.highSince = time.Time{}
		a.tier, a.wantTier, a.tierSince = 0, 0, now
		c.autoChange(now, "spotlight", 0.5)
		return
	}

	// the energy settles in another tier for a few seconds
	if t := tierOf(a.energy); t != a.wantTier {
		a.wantTier, a.tierSince = t, now
	}
	if a.wantTier != a.tier && now.Sub(a.tierSince) > 3*time.Second && sinceChange > 4*time.Second {
		a.tier = a.wantTier
		c.autoChange(now, "", []float64{3, 1.5, 0.5}[a.tier])
		return
	}

	// a new phrase: every 32 beats with a beat, or every 25 seconds; a
	// spotlight moment lasts about ten seconds
	phrase := sinceChange > 25*time.Second || a.moment == "spotlight" && sinceChange > 10*time.Second
	if c.beatLive(now) {
		phrase = phrase || c.beats.position(now)-a.beatAt >= 32 && sinceChange > 8*time.Second
	}
	if phrase {
		c.autoChange(now, "", []float64{4, 2, 0.3}[a.tier])
		return
	}

	// keep the speed with the music
	c.autoSpeed(now)
}

// autoChange must be called with mu held. It picks the next look.
func (c *Controller) autoChange(now time.Time, moment string, fadeSec float64) {
	a := &c.auto
	var sl *showLook
	if moment == "spotlight" {
		sl = findShowLook("Spotlight show")
	} else {
		names := autoTiers[a.tier]
		pick := names[a.rng.Intn(len(names))]
		for i := 0; i < 4 && (pick == a.look || pick == "Spotlight show" && a.moment == "spotlight"); i++ {
			pick = names[a.rng.Intn(len(names))]
		}
		sl = findShowLook(pick)
	}
	if sl == nil {
		return
	}
	look := *sl
	// vary the spots so the same look doesn't always move the same way
	if look.spots.level > 0 && moment != "spotlight" {
		moves := autoMoves[a.tier]
		look.spots.pattern = moves[a.rng.Intn(len(moves))]
		look.spots.size = 40 + 20*float64(a.tier) + 20*a.rng.Float64()
	}
	m := c.buildLook(look)
	if len(m.Fixtures) == 0 {
		return
	}
	c.playMood(&m, "", fadeSec)
	c.playing = "Auto · " + look.name
	a.look, a.moment, a.changed = look.name, moment, now
	a.beatAt = c.beats.position(now)
	a.lastSpeed = -1
	c.autoSpeed(now)
}

// autoSpeed must be called with mu held. Quiet music runs slow, loud fast.
func (c *Controller) autoSpeed(now time.Time) {
	a := &c.auto
	speed := math.Round(10 + 80*a.energy)
	music := c.beatLive(now) && c.autoListening(now)
	if music {
		// with a beat the speed picks how many changes per beat
		speed = math.Round(20 + 60*a.energy)
	}
	if math.Abs(speed-a.lastSpeed) < 3 && music == a.lastMusic {
		return
	}
	a.lastSpeed, a.lastMusic = speed, music
	for _, r := range c.runs {
		r.rebase(now)
		r.Speed, r.Music = speed, music
	}
	for _, r := range c.moves {
		r.Speed, r.Music = speed, music
	}
}

func findShowLook(name string) *showLook {
	for i := range showLooks {
		if showLooks[i].name == name {
			return &showLooks[i]
		}
	}
	return nil
}

// autoInfo must be called with mu held.
func (c *Controller) autoInfo() AutoState {
	a := c.auto
	s := AutoState{On: a.on, Sensitivity: a.sensitivity, Listening: c.autoListening(c.now()),
		Energy: int(math.Round(a.energy * 100)), Level: autoTierNames[a.tier], Look: a.look, Moment: a.moment}
	if !a.on {
		s.Look, s.Moment = "", ""
	} else if c.now().Sub(a.changed) > 6*time.Second {
		s.Moment = ""
	}
	return s
}
