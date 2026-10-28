package controller

import (
	"fmt"
	"testing"
	"time"
)

func uplights() []int { return []int{0, 1, 2, 3, 4, 5} }

// newSplit is the default rig with the uplights moved to universe 1, so the
// wash (17-19) on universe 0 is not mixed with them.
func newSplit(t *testing.T) (*Controller, *time.Time) {
	c, now := newTest(t)
	if err := c.UpdateConfig(ConfigUpdate{Universe: ptr(1)}); err != nil {
		t.Fatal(err)
	}
	return c, now
}

func TestEveryProgramRuns(t *testing.T) {
	for _, p := range Programs {
		c, now := newSplit(t)
		if err := c.StartProgram(nil, ProgramRun{Program: p.ID, Speed: 50, Size: 100}); err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		seen := map[[3]byte]bool{}
		for step := 0; step < 40; step++ {
			f := c.Frames()[0]
			seen[[3]byte{f[16], f[17], f[18]}] = true // the wash
			*now = now.Add(97 * time.Millisecond)
		}
		if len(seen) < 2 {
			t.Errorf("%s: wash never changed (%v)", p.ID, seen)
		}
	}
}

func TestProgramTargetsAndStop(t *testing.T) {
	c, _ := newTest(t)
	if err := c.StartProgram(uplights(), ProgramRun{Program: "chase", Speed: 50}); err != nil {
		t.Fatal(err)
	}
	if err := c.StartProgram([]int{6, 7}, ProgramRun{Program: "fire"}); err != nil {
		t.Fatal(err) // the spot (7) has no color and is skipped
	}
	runs := c.State().Perform.Runs
	if len(runs) != 2 || len(runs[0].Fixtures) != 6 || len(runs[1].Fixtures) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	// starting on all takes every fixture over
	c.StartProgram(nil, ProgramRun{Program: "rainbow"})
	if runs := c.State().Perform.Runs; len(runs) != 1 || len(runs[0].Fixtures) != 7 {
		t.Fatalf("after all: %+v", runs)
	}
	// a color pick on the wash pulls it out of the program
	c.SetColor([]int{6}, ColorSet{R: ptr(255), G: ptr(0), B: ptr(0)})
	if runs := c.State().Perform.Runs; len(runs[0].Fixtures) != 6 {
		t.Fatalf("wash should leave the run: %+v", runs)
	}
	c.StopProgram(nil)
	if len(c.State().Perform.Runs) != 0 {
		t.Error("stop all left runs")
	}
	if c.StartProgram([]int{7}, ProgramRun{Program: "chase"}) == nil {
		t.Error("spot only: expected no color fixtures error")
	}
	if c.StartProgram(nil, ProgramRun{Program: "nope"}) == nil {
		t.Error("expected unknown program error")
	}
}

func TestMusicModeFollowsBeats(t *testing.T) {
	c, now := newTest(t)
	c.StartProgram([]int{6}, ProgramRun{Program: "jump", Speed: 50, Music: true, Variation: 4}) // red/blue, 1 step per beat
	for i := 0; i < 4; i++ {
		c.Beat("tap")
		*now = now.Add(500 * time.Millisecond)
	}
	if bpm := c.State().Perform.Beat.BPM; bpm != 120 {
		t.Fatalf("tap tempo = %v BPM, want 120", bpm)
	}
	c.Beat("tap")
	a := c.Frames()[0]
	*now = now.Add(520 * time.Millisecond)
	b := c.Frames()[0]
	if a[16] == b[16] && a[18] == b[18] {
		t.Errorf("jump should change color on the next beat: %v -> %v", a[16:19], b[16:19])
	}
}

func TestMicBeatsFoldIntoRange(t *testing.T) {
	var b BeatClock
	now := time.Unix(0, 0)
	for i := 0; i < 20; i++ {
		b.beat(now, "mic")
		now = now.Add(250 * time.Millisecond) // 240 BPM hits fold to 120
	}
	if s := b.state(now); s.BPM < 115 || s.BPM > 125 {
		t.Fatalf("bpm = %v", s.BPM)
	}
}

func TestGroupAndPosition(t *testing.T) {
	c, _ := newTest(t)
	if err := c.SetGroup(uplights(), ptr(30), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPosition(6, 0.5, 2); err != nil {
		t.Fatal(err)
	}
	s := c.State()
	if s.Fixtures[3].Brightness != 30 || s.Fixtures[6].Brightness != 100 {
		t.Error("group brightness wrong")
	}
	if p := s.Fixtures[6].Pos; p == nil || p.X != 0.5 || p.Y != 1 {
		t.Errorf("pos = %+v", p)
	}
}

func TestGroupFlash(t *testing.T) {
	c, _ := newSplit(t)
	c.SetFlash(true, []int{6})
	frames := c.Frames()
	if f := frames[0]; f[16] != 255 || f[17] != 255 {
		t.Fatalf("group flash: wash r=%d g=%d", f[16], f[17])
	}
	if f := frames[1]; f[4] != 0 {
		t.Fatalf("uplight 1 white should stay off, got %d", f[4])
	}
}

func TestLiveColorsFollowPrograms(t *testing.T) {
	c, now := newSplit(t)
	if err := c.StartProgram(nil, ProgramRun{Program: "chase", Speed: 50, Size: 100}); err != nil {
		t.Fatal(err)
	}
	c.Frames()
	first := c.State().Perform.Live
	if len(first) != len(c.State().Fixtures) {
		t.Fatalf("live has %d colors for %d fixtures", len(first), len(c.State().Fixtures))
	}
	for step := 0; step < 10; step++ {
		*now = now.Add(300 * time.Millisecond)
		c.Frames()
		if fmt.Sprint(c.State().Perform.Live) != fmt.Sprint(first) {
			return
		}
	}
	t.Errorf("live colors did not move with the chase: %v", first)
}
