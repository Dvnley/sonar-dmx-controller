package controller

import (
	"math"
	"testing"
	"time"
)

func TestDefaultRigIsDanleys(t *testing.T) {
	c, err := New("", &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	s := c.State()
	if !s.Show.RigOK || len(s.Conflicts) != 0 {
		t.Fatalf("rig ok %v, conflicts %v", s.Show.RigOK, s.Conflicts)
	}
	frame := c.Frames()[0]
	// spots: brightness on ch 6, aimed at the middle on ch 1 and 3
	if frame[0] != 128 || frame[2] != 128 || frame[5] != 255 {
		t.Fatalf("spot channels %v", frame[:11])
	}
	// spider at 34: brightness open on its ch 3
	if frame[33+2] != 255 {
		t.Fatalf("spider brightness %d", frame[35])
	}
}

func TestSetupRigAndMovement(t *testing.T) {
	c, now := newTest(t)
	if c.State().Show.RigOK {
		t.Fatal("legacy rig should not count as set up")
	}
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	s := c.State()
	if !s.Show.RigOK {
		t.Fatal("rig not set up")
	}
	spot, spider := -1, -1
	for i, f := range s.Fixtures {
		switch f.Profile {
		case "gobo-spot-11ch":
			spot = i
		case "spider-10ch":
			spider = i
		}
	}
	if err := c.StartMovement(nil, MoveRun{Pattern: "sweep", Speed: 100, Size: 100}); err != nil {
		t.Fatal(err)
	}
	if got := c.State().Show.Moves[0].Fixtures; len(got) != 2 || got[0] != spot || got[1] != spider {
		t.Fatalf("movement on %v, want spot %d and spider %d", got, spot, spider)
	}
	// 240 BPM, 16 beats per cycle: a quarter cycle is 1 s
	c.Frames()
	*now = now.Add(time.Second)
	f := c.Frames()[0]
	if f[2] < 250 || f[0] != 128 {
		t.Fatalf("sweep should push left/right (ch 3) to the edge, not move ch 1: %v", f[:4])
	}
	// the spider's two bars swing against each other
	a, b := int(f[33]), int(f[34])
	if a+b < 250 || a+b > 258 || math.Abs(float64(a-b)) < 100 {
		t.Fatalf("spider bars %d %d", a, b)
	}

	// changing the speed must not make the lights jump
	before := c.Frames()[0][2]
	c.SetShowSpeed(0, false)
	if after := c.Frames()[0][2]; after != before {
		t.Fatalf("jumped from %d to %d", before, after)
	}
	if c.State().Show.Speed != 0 || c.State().Show.Moves[0].Speed != 0 {
		t.Fatal("speed not applied")
	}

	if err := c.StopMovement(nil); err != nil {
		t.Fatal(err)
	}
	if f := c.Frames()[0]; f[2] != 128 {
		t.Fatalf("stop should go back to the aim, got %d", f[2])
	}
	if err := c.StartMovement([]int{0}, MoveRun{Pattern: "circle"}); err == nil {
		t.Fatal("an uplight has nothing to move")
	}
}

func TestSpeedChangeKeepsProgramPlace(t *testing.T) {
	c, now := newSplit(t)
	if err := c.StartProgram(nil, ProgramRun{Program: "fade", Speed: 60, Size: 50}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(7 * time.Second)
	before := c.Frames()[0][16]
	c.SetShowSpeed(10, false)
	if after := c.Frames()[0][16]; after != before {
		t.Fatalf("program jumped from %d to %d", before, after)
	}
}

func TestShowGroupOpensDimmer(t *testing.T) {
	c, _ := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	spot := 7
	if err := c.UpdateFixture(FixtureUpdate{Index: spot, Values: map[string]int{"dimmer": 0}}); err != nil {
		t.Fatal(err)
	}
	off, on, lvl := false, true, 40
	if err := c.ShowGroup([]int{spot}, &off, nil); err != nil {
		t.Fatal(err)
	}
	if c.Frames()[0][5] != 0 {
		t.Fatal("off should be dark")
	}
	if err := c.ShowGroup([]int{spot}, &on, &lvl); err != nil {
		t.Fatal(err)
	}
	if got := c.Frames()[0][5]; got != 102 {
		t.Fatalf("on at 40%% should give 102, got %d", got)
	}
}

func TestHandFaders(t *testing.T) {
	c, _ := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFaders(0, map[int]int{17: 10, 300: 200}); err != nil {
		t.Fatal(err)
	}
	f := c.Frames()[0]
	if f[16] != 10 || f[299] != 200 {
		t.Fatalf("faders not applied: ch17=%d ch300=%d", f[16], f[299])
	}
	if s := c.State().Faders; s.Set[17] != 10 || s.Output[299] != 200 {
		t.Fatalf("state %v", s.Set)
	}
	on := true
	c.SetBlackout(&on)
	if c.Frames()[0][299] != 0 {
		t.Fatal("blackout must win over faders")
	}
	off := false
	c.SetBlackout(&off)
	c.ReleaseFaders(0, []int{17})
	if f := c.Frames()[0]; f[16] != 0 || f[18] != 255 || f[299] != 200 {
		t.Fatalf("release 17: got %d %d %d (wash should be back to blue)", f[16], f[18], f[299])
	}
	c.ReleaseFaders(0, nil)
	if c.Frames()[0][299] != 0 {
		t.Fatal("release all")
	}
	if err := c.SetFader(0, 513, 1); err == nil {
		t.Fatal("513 accepted")
	}
}
