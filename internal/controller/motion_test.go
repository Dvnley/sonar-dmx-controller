package controller

import (
	"testing"
	"time"
)

func setRed(t *testing.T, c *Controller, r int) {
	t.Helper()
	z := 0
	if err := c.SetColor(nil, ColorSet{R: &r, G: &z, B: &z}); err != nil {
		t.Fatal(err)
	}
	c.Frames()
}

func TestMarkPointsAndGlide(t *testing.T) {
	c, now := newTest(t)
	setRed(t, c, 0)
	id, _ := c.SaveMood("", "Glide")
	for _, r := range []int{0, 200} {
		setRed(t, c, r)
		if _, err := c.MarkPoint(id); err != nil {
			t.Fatal(err)
		}
	}
	// speed 100 = 0.5 s per point, no hold
	if err := c.SetMotion(id, Motion{Speed: 100, Hold: 0}); err != nil {
		t.Fatal(err)
	}
	c.PlayMood(id, 0)
	c.Frames()
	if c.cycle == nil {
		t.Fatal("mood with two points should move")
	}
	*now = now.Add(250 * time.Millisecond) // halfway from point 1 to 2
	c.Frames()
	if v := red(c, 0); v < 80 || v > 120 {
		t.Fatalf("halfway red = %d", v)
	}
	*now = now.Add(250 * time.Millisecond)
	c.Frames()
	if v := red(c, 0); v != 200 {
		t.Fatalf("at point 2 red = %d", v)
	}
	*now = now.Add(500 * time.Millisecond) // loops back to point 1
	c.Frames()
	if v := red(c, 0); v != 0 {
		t.Fatalf("back at point 1 red = %d", v)
	}
	// updating the mood with the current look keeps its points
	c.SaveMood(id, "")
	if n := len(c.cfg.Moods[0].Points); n != 2 {
		t.Fatalf("points lost: %d", n)
	}
	// touching a color by hand stops the motion
	setRed(t, c, 50)
	if c.cycle != nil {
		t.Fatal("hand change should stop the moving mood")
	}
	if err := c.EditPoint(id, 0, true, 0); err != nil || len(c.cfg.Moods[0].Points) != 1 {
		t.Fatalf("delete point: %v", err)
	}
	if s := c.State(); s.Moods[0].MoveTxt != "0.5 s per point" {
		t.Fatalf("label %q", s.Moods[0].MoveTxt)
	}
}

func TestBeatDrivesMotionAndPulse(t *testing.T) {
	c, now := newTest(t)
	id, _ := c.SaveMood("", "Beat")
	for _, r := range []int{0, 255} {
		setRed(t, c, r)
		c.MarkPoint(id)
	}
	c.SetMotion(id, Motion{Speed: 100, Beat: true, Pulse: 100}) // one point per beat
	c.PlayMood(id, 0)
	c.Frames()
	// 120 BPM taps
	for i := 0; i < 4; i++ {
		c.Beat("tap")
		*now = now.Add(500 * time.Millisecond)
	}
	c.Frames()
	before := c.cycle.phase
	*now = now.Add(250 * time.Millisecond) // half a beat later
	c.Frames()
	if d := c.cycle.phase - before; d < 0.45 || d > 0.55 {
		if d < 0 {
			d += 2
		}
		if d < 0.45 || d > 0.55 {
			t.Fatalf("moved %.2f points in half a beat", d)
		}
	}
	// pulse: level dips between beats
	f := c.live[0]
	if f.Brightness >= 100 {
		t.Fatalf("pulse should dim between beats, brightness %d", f.Brightness)
	}
	// no music for a while: back to the clock and no pulse
	*now = now.Add(10 * time.Second)
	c.Frames()
	if c.beatLive(*now) {
		t.Fatal("beat should have gone stale")
	}
}

func TestStarterMovingMood(t *testing.T) {
	c, _ := newTest(t)
	c.AddStarterMoods()
	var sunset *Mood
	for i := range c.cfg.Moods {
		if c.cfg.Moods[i].Name == "Sunset drift" {
			sunset = &c.cfg.Moods[i]
		}
	}
	if sunset == nil || len(sunset.Points) != 4 {
		t.Fatal("Sunset drift should have 4 points")
	}
	c.PlayMood(sunset.ID, 0)
	if c.cycle == nil {
		t.Fatal("Sunset drift should move")
	}
}
