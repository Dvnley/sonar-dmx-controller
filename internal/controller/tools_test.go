package controller

import (
	"testing"
	"time"
)

func rigTest(t *testing.T) (*Controller, *time.Time) {
	t.Helper()
	c, now := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	return c, now
}

func TestAimAndSavedSpots(t *testing.T) {
	c, _ := rigTest(t)
	if err := c.SetAim(nil, 0, 1); err != nil {
		t.Fatal(err)
	}
	f := c.Frames()[0]
	if f[0] != 255 || f[2] != 0 { // spots: ch1 up/down, ch3 left/right
		t.Fatalf("aim %v", f[:4])
	}
	if err := c.SaveAim("Dance floor", false); err != nil {
		t.Fatal(err)
	}
	s := c.State().Tools
	if len(s.AimPoints) != 1 || s.AimPoints[0].X != 0 || s.AimPoints[0].Y != 1 {
		t.Fatalf("points %+v", s.AimPoints)
	}
	if err := c.SaveAim("dance floor", true); err != nil || len(c.State().Tools.AimPoints) != 0 {
		t.Fatal("remove", err)
	}
	if err := c.SetAim([]int{0}, .5, .5); err == nil {
		t.Fatal("the wash does not move")
	}
}

func TestColorHit(t *testing.T) {
	c, _ := rigTest(t)
	if err := c.SetHit(true, "red", nil); err != nil {
		t.Fatal(err)
	}
	f := c.Frames()[0]
	if f[16] != 255 || f[17] != 0 || f[34] != 255 || f[37] != 0 { // wash R at 17, spider R at 35
		t.Fatalf("red hit %v %v", f[16:19], f[33:43])
	}
	_ = c.SetHit(true, "uv", nil)
	if f := c.Frames()[0]; f[39] != 255 { // spider UV on its ch 7 (40)
		t.Fatalf("uv hit %v", f[33:43])
	}
	_ = c.SetHit(false, "uv", nil)
	if c.State().Tools.HitColor != "" {
		t.Fatal("hit should end")
	}
	if c.SetHit(true, "plaid", nil) == nil {
		t.Fatal("bad color")
	}
}

func TestUndoGoesBackAndForth(t *testing.T) {
	c, _ := rigTest(t)
	c.AddStarterMoods()
	a, b := c.cfg.Moods[0], c.cfg.Moods[3]
	_ = c.PlayMood(a.ID, 0)
	_ = c.PlayMood(b.ID, 0)
	if err := c.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := c.State().Schedule.Playing; got != a.Name {
		t.Fatalf("back to %q, want %q", got, a.Name)
	}
	_ = c.Undo()
	if got := c.State().Schedule.Playing; got != b.Name {
		t.Fatalf("forward to %q, want %q", got, b.Name)
	}
}

func TestLightCheckWalksEachLight(t *testing.T) {
	c, now := rigTest(t)
	c.StartCheck(false)
	s := c.State().Tools.Check
	if !s.Running || s.Light != "Chauvet wash" || s.Doing != "red" {
		t.Fatalf("start %+v", s)
	}
	f := c.Frames()[0]
	if f[16] != 255 || f[5] != 0 || f[34] != 0 {
		t.Fatalf("only the wash should be red: %v", f[:45])
	}
	*now = now.Add(5*checkStepTime + checkStepTime/2) // spots, "left"
	s = c.State().Tools.Check
	if s.Light != "Spots" || s.Doing != "left" {
		t.Fatalf("step %+v", s)
	}
	if f := c.Frames()[0]; f[2] != 51 || f[5] != 255 {
		t.Fatalf("spots left %v", f[:10])
	}
	*now = now.Add(time.Minute)
	if c.State().Tools.Check.Running {
		t.Fatal("check should end")
	}
}

func TestScheduleCanTurnOnAutoAndItSurvivesARestart(t *testing.T) {
	c, now := rigTest(t)
	c.AddStarterMoods()
	at := now.Add(-time.Minute).In(c.schedLocation()).Format("15:04")
	if err := c.SetSchedule(Schedule{Enabled: true, Entries: []ScheduleEntry{{Time: at, Mood: MoodAuto}}}); err != nil {
		t.Fatal(err)
	}
	c.CheckSchedule()
	if !c.State().Auto.On || !c.cfg.AutoOn {
		t.Fatal("the schedule should turn Auto on")
	}
	off := false
	c.SetAuto(&off, nil)
	if c.cfg.AutoOn {
		t.Fatal("off should be remembered")
	}
}
