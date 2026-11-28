package controller

import (
	"testing"
	"time"
)

func TestAutoFollowsTheMusic(t *testing.T) {
	c, now := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	on := true
	c.SetAuto(&on, nil)
	if !c.State().Auto.On || len(c.State().Perform.Runs) == 0 {
		t.Fatal("auto should start a look")
	}
	feed := func(level float64, secs float64) {
		for i := 0; i < int(secs*4); i++ {
			*now = now.Add(250 * time.Millisecond)
			c.AutoLevel(level)
			c.autoTick()
		}
	}
	inTier := func(t int) bool {
		for _, n := range autoTiers[t] {
			if n == c.State().Auto.Look {
				return true
			}
		}
		return false
	}
	// a quiet song after a loud one: calm and slow
	feed(0.3, 10)
	feed(0.02, 20)
	s := c.State()
	if s.Auto.Level != "Calm" || !s.Auto.Listening {
		t.Fatalf("quiet: %+v", s.Auto)
	}
	if !inTier(0) || s.Auto.Moment != "" {
		t.Fatalf("quiet look %q", s.Auto.Look)
	}
	slow := s.Perform.Runs[0].Speed
	// the drop
	feed(0.3, 1.5)
	s = c.State()
	if s.Auto.Moment != "drop" || !inTier(2) {
		t.Fatalf("drop: %+v", s.Auto)
	}
	feed(0.3, 6)
	if fast := c.State().Perform.Runs[0].Speed; fast <= slow+30 {
		t.Fatalf("speed %v -> %v", slow, fast)
	}
	// loud for a while, then a break: spotlight
	feed(0.3, 10)
	feed(0.01, 3)
	if s := c.State().Auto; s.Look != "Spotlight show" || s.Moment != "spotlight" {
		t.Fatalf("break: %+v", s)
	}
	// blackout wins
	bo := true
	c.SetBlackout(&bo)
	feed(0.3, 30)
	if !c.State().Blackout {
		t.Fatal("auto undid the blackout")
	}
	bo = false
	c.SetBlackout(&bo)
	// a mood picked by hand ends Auto
	c.AddStarterMoods()
	if err := c.PlayMood(c.cfg.Moods[0].ID, 0); err != nil {
		t.Fatal(err)
	}
	if c.State().Auto.On {
		t.Fatal("auto should stop")
	}
}

func TestAutoWithoutMicStillChangesLooks(t *testing.T) {
	c, now := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	on := true
	c.SetAuto(&on, nil)
	seen := map[string]bool{}
	for i := 0; i < 4*200; i++ {
		*now = now.Add(250 * time.Millisecond)
		c.autoTick()
		seen[c.State().Auto.Look] = true
	}
	if len(seen) < 3 {
		t.Fatalf("looks %v", seen)
	}
}

func TestWaterUsesTheSpiderWhite(t *testing.T) {
	c, now := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	if err := c.StartProgram([]int{0, 2}, ProgramRun{Program: "water", Speed: 50, Size: 100}); err != nil {
		t.Fatal(err)
	}
	whites, blues := 0, 0
	for i := 0; i < 400; i++ {
		*now = now.Add(50 * time.Millisecond)
		f := c.Frames()[0]
		if f[37] > 100 { // spider white (ch 5 at 34)
			whites++
		}
		if f[18] > 150 { // wash blue (ch 3 at 17)
			blues++
		}
	}
	if whites == 0 || blues < 100 {
		t.Fatalf("whites %d, blues %d", whites, blues)
	}
}
