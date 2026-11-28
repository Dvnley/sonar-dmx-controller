package controller

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
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
	// spider at 34 starts white, on its ch 5 (38)
	if frame[37] != 255 || frame[34] != 0 {
		t.Fatalf("spider channels %v", frame[33:43])
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
	spot := -1
	for i, f := range s.Fixtures {
		if f.Profile == "gobo-spot-11ch" {
			spot = i
		}
	}
	if err := c.StartMovement(nil, MoveRun{Pattern: "sweep", Speed: 100, Size: 100}); err != nil {
		t.Fatal(err)
	}
	if got := c.State().Show.Moves[0].Fixtures; len(got) != 1 || got[0] != spot {
		t.Fatalf("movement on %v, want only the spot %d", got, spot)
	}
	// 240 BPM, 16 beats per cycle: a quarter cycle is 1 s
	c.Frames()
	*now = now.Add(time.Second)
	f := c.Frames()[0]
	if f[2] < 250 || f[0] != 128 {
		t.Fatalf("sweep should push left/right (ch 3) to the edge, not move ch 1: %v", f[:4])
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

func TestOldSpotPatchMovesTo10CH(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	old := `{"fixtures":[{"name":"Spots","profile":"gobo-spot-11ch","mode":"11CH","address":1,"enabled":true,"brightness":100,
	  "channels":[{"name":"Up / down","type":"tilt"},{"name":"f","type":"tilt_fine"},{"name":"Left / right","type":"pan"},{"name":"f","type":"pan_fine"},
	  {"name":"s","type":"speed"},{"name":"Brightness","type":"dimmer"},{"name":"st","type":"shutter"},{"name":"Color wheel","type":"color"},
	  {"name":"Gobo","type":"gobo"},{"name":"a","type":"auto"},{"name":"r","type":"reset"}],
	  "values":[40,0,90,0,0,255,0,30,60,0,0]}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	f := c.State().Fixtures[0]
	if len(f.Channels) != 10 || f.Mode != "10CH" || f.Channels[8].Type != "color" || f.Channels[9].Type != "gobo" {
		t.Fatalf("not migrated: %+v", f.Channels)
	}
	if v := f.Values; v[0] != 40 || v[2] != 90 || v[5] != 255 || v[8] != 30 || v[9] != 60 {
		t.Fatalf("values not kept: %v", v)
	}
}

func TestMoodKeepsMovement(t *testing.T) {
	c, _ := newTest(t)
	if err := c.SetupRig(); err != nil {
		t.Fatal(err)
	}
	if err := c.StartMovement(nil, MoveRun{Pattern: "figure8", Speed: 30, Size: 60}); err != nil {
		t.Fatal(err)
	}
	id, err := c.SaveMood("", "Moving")
	if err != nil {
		t.Fatal(err)
	}
	c.StopMovement(nil)
	if err := c.PlayMood(id, 0); err != nil {
		t.Fatal(err)
	}
	if mv := c.State().Show.Moves; len(mv) != 1 || mv[0].Pattern != "figure8" || len(mv[0].Fixtures) != 1 {
		t.Fatalf("moves %+v", mv)
	}
}

func TestTwoBarLightSwingsBothBars(t *testing.T) {
	f, _ := fixture.New("Bars", "generic-rgb", "", 0, 1)
	f.Channels = []fixture.Channel{{Name: "a", Type: fixture.Tilt}, {Name: "b", Type: fixture.Tilt}}
	f.Values = fixture.Values{128, 128}
	moveValues(&f, 1, 0, 100)
	a, b := int(f.Values[0]), int(f.Values[1])
	if a+b < 250 || a+b > 258 || math.Abs(float64(a-b)) < 100 {
		t.Fatalf("bars %d %d", a, b)
	}
}

func TestOldSpiderGuessMovesToCheckedMap(t *testing.T) {
	c, _ := newTest(t)
	old := fixture.Fixture{Name: "Spider", Profile: "spider-10ch", Mode: "10CH", Address: 34, Enabled: true, Brightness: 100,
		Channels: []fixture.Channel{{Type: fixture.Tilt}, {Type: fixture.Tilt}, {Type: fixture.Dimmer}, {Type: fixture.Shutter},
			{Type: fixture.Red}, {Type: fixture.Green}, {Type: fixture.Blue}, {Type: fixture.White}, {Type: fixture.Speed}, {Type: fixture.Auto}},
		Values: fixture.Values{128, 128, 255, 0, 0, 0, 255, 0, 0, 0}}
	c.cfg.Fixtures = []fixture.Fixture{old}
	c.normalize()
	f := c.cfg.Fixtures[0]
	if f.Channels[1].Type != fixture.Red || f.Channels[6].Type != fixture.UV || f.Values[3] != 255 {
		t.Fatalf("not moved: %+v %v", f.Channels, f.Values)
	}
	if fr := c.Frames()[0]; fr[36] != 255 || fr[39] != 0 {
		t.Fatalf("blue should be on 37: %v", fr[33:43])
	}
}
