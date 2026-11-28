package controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

func red(c *Controller, i int) uint8 {
	f := c.live[i]
	return f.Values[f.Find(fixture.Red)]
}

func TestMoodSavePlay(t *testing.T) {
	c, now := newTest(t)
	r, g, b := 255, 0, 0
	if err := c.SetColor(nil, ColorSet{R: &r, G: &g, B: &b}); err != nil {
		t.Fatal(err)
	}
	id, err := c.SaveMood("", "Red night")
	if err != nil || id != "mood-red-night" {
		t.Fatalf("id %q err %v", id, err)
	}
	if err := c.StartProgram(nil, ProgramRun{Program: "jump", Speed: 50}); err != nil {
		t.Fatal(err)
	}
	party, _ := c.SaveMood("", "Party")
	c.Frames()
	if err := c.PlayMood(id, 0); err != nil {
		t.Fatal(err)
	}
	c.Frames()
	if red(c, 0) != 255 || len(c.runs) != 0 {
		t.Fatalf("red mood: red=%d runs=%d", red(c, 0), len(c.runs))
	}
	if err := c.PlayMood(party, 10); err != nil {
		t.Fatal(err)
	}
	if len(c.runs) != 1 || c.runs[0].Program != "jump" || c.xfade == nil {
		t.Fatalf("party mood: runs %+v", c.runs)
	}
	*now = now.Add(11 * time.Second)
	c.Frames()
	if c.xfade != nil {
		t.Fatal("crossfade should have ended")
	}
	if s := c.State(); s.Schedule.Playing != "Party" || !s.Schedule.ByHand || len(s.Moods) != 2 || len(s.Moods[1].Colors) == 0 {
		t.Fatalf("state %+v moods %+v", s.Schedule, s.Moods)
	}
}

func TestCrossfadeBlends(t *testing.T) {
	c, now := newTest(t)
	r, z := 255, 0
	c.SetColor(nil, ColorSet{R: &z, G: &z, B: &z})
	dark, _ := c.SaveMood("", "Dark")
	c.SetColor(nil, ColorSet{R: &r, G: &z, B: &z})
	bright, _ := c.SaveMood("", "Bright")
	c.PlayMood(dark, 0)
	c.Frames()
	c.PlayMood(bright, 10)
	*now = now.Add(5 * time.Second)
	c.Frames()
	if v := red(c, 0); v < 100 || v > 155 {
		t.Fatalf("halfway red = %d", v)
	}
}

func TestScheduleOccurrence(t *testing.T) {
	c, now := newTest(t)
	loc, _ := time.LoadLocation("America/New_York")
	c.AddStarterMoods()
	if len(c.cfg.Moods) != len(starters)+len(showLooks) || c.AddStarterMoods() != 0 {
		t.Fatalf("starters: %d", len(c.cfg.Moods))
	}
	chill, party := c.cfg.Moods[1].ID, c.cfg.Moods[3].ID
	err := c.SetSchedule(Schedule{Enabled: true, TimeZone: "America/New_York", Entries: []ScheduleEntry{
		{Time: "23:00", Mood: party, Fade: 30},
		{Time: "21:00", Mood: chill, Fade: 5},
		{Time: "2:30", Mood: MoodOff, Days: []int{6, 0}}, // Sat and Sun mornings
	}})
	if err != nil {
		t.Fatal(err)
	}
	if c.cfg.Schedule.Entries[0].Time != "02:30" {
		t.Fatalf("not sorted: %+v", c.cfg.Schedule.Entries)
	}
	// Friday 2026-11-20 22:15 in New York
	*now = time.Date(2026, 11, 20, 22, 15, 0, 0, loc)
	c.CheckSchedule()
	if c.playing != "Chill blue" || c.xfade != nil {
		t.Fatalf("launch should resume chill without a fade: %q %v", c.playing, c.xfade)
	}
	s := c.State().Schedule
	if s.Next == nil || s.Next.Name != "Party" || s.Next.Clock != "Fri 11:00 PM" {
		t.Fatalf("next %+v", s.Next)
	}
	c.Frames()
	*now = time.Date(2026, 11, 20, 23, 0, 1, 0, loc)
	c.CheckSchedule()
	if c.playing != "Party" || c.xfade == nil {
		t.Fatalf("23:00 should fade to party: %q", c.playing)
	}
	// Saturday 02:30 -> off
	*now = time.Date(2026, 11, 21, 2, 31, 0, 0, loc)
	c.CheckSchedule()
	c.Frames()
	if c.playing != "Lights off" || !c.blackout {
		t.Fatalf("2:30 Sat should be off: %q", c.playing)
	}
	// Tuesday 02:31 -> no off entry, still party from Monday 23:00
	*now = time.Date(2026, 11, 24, 2, 31, 0, 0, loc)
	c.CheckSchedule()
	if c.playing != "Party" || c.blackout {
		t.Fatalf("Tue 2:31 should be party: %q", c.playing)
	}
	// playing by hand sticks until the next time
	c.PlayMood(chill, 0)
	c.CheckSchedule()
	if c.playing != "Chill blue" {
		t.Fatal("schedule overrode a hand-picked mood before its next time")
	}
	if err := c.SetSchedule(Schedule{Entries: []ScheduleEntry{{Time: "25:00", Mood: chill}}}); err == nil {
		t.Fatal("bad time accepted")
	}
	if err := c.SetSchedule(Schedule{Entries: []ScheduleEntry{{Time: "10:00", Mood: "nope"}}}); err == nil {
		t.Fatal("unknown mood accepted")
	}
	c.DeleteMood(party)
	for _, e := range c.cfg.Schedule.Entries {
		if e.Mood == party {
			t.Fatal("deleted mood left in schedule")
		}
	}
}

func TestScheduleResumesAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	c.AddStarterMoods()
	if err := c.SetSchedule(Schedule{Enabled: true, TimeZone: "UTC", Entries: []ScheduleEntry{{Time: "00:00", Mood: c.cfg.Moods[3].ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	c2.CheckSchedule()
	if c2.playing != "Party" || len(c2.runs) != 1 {
		t.Fatalf("after restart: %q runs %d", c2.playing, len(c2.runs))
	}
}
