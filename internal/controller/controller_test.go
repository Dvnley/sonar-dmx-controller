package controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/artnet"
	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

type fakeNet struct{ sent map[int][]byte }

func (f *fakeNet) SendDmx(_ string, u int, d []byte) error {
	if f.sent == nil {
		f.sent = map[int][]byte{}
	}
	f.sent[u] = d
	return nil
}
func (f *fakeNet) Poll(...string) error            { return nil }
func (f *fakeNet) Seen(string, time.Duration) bool { return false }
func (f *fakeNet) Nodes() []artnet.Node            { return nil }

func newTest(t *testing.T) (*Controller, *time.Time) {
	t.Helper()
	c, err := New("", &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	return c, &now
}

func TestParseScriptMatchesProbe(t *testing.T) {
	steps, err := ParseScript("5:17=255;5:18=255;5:19=255;5:17=255,18=255,19=255;2:")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 5 || steps[3].Label != "17=255,18=255,19=255" || steps[4].Label != "all zero" {
		t.Fatalf("got %+v", steps)
	}
	for _, bad := range []string{"x:1=2", "1:0=5", "1:1=256", "1:1", ""} {
		if _, err := ParseScript(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestSweepPlan(t *testing.T) {
	steps, err := SweepPlan{From: 1, To: 9, Value: 255, Hold: 4, Gap: 2}.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 19 { // lead-in + 9 x (channel, gap)
		t.Fatalf("got %d steps", len(steps))
	}
	steps, _ = SweepPlan{From: 3, To: 4, Value: 200, Base: "1=128,6=255", Manual: true}.Build()
	if len(steps) != 2 || steps[0].Values[3] != 200 || steps[0].Values[1] != 128 || steps[1].Values[6] != 255 {
		t.Fatalf("manual plan = %+v", steps)
	}
}

func TestSweepTakesOverOutput(t *testing.T) {
	c, now := newTest(t)
	c.SetColor(nil, ColorSet{R: ptr(255)})
	steps, _ := ParseScript("1:17=255;1:18=255")
	if err := c.StartSweep(Sweep{Universe: 0, Steps: steps}); err != nil {
		t.Fatal(err)
	}
	f := c.Frames()[0]
	if f[16] != 255 || f[0] != 0 {
		t.Fatalf("sweep frame wrong: ch17=%d ch1=%d", f[16], f[0])
	}
	*now = now.Add(1500 * time.Millisecond)
	if f := c.Frames()[0]; f[17] != 255 || f[16] != 0 {
		t.Fatalf("second step wrong")
	}
	*now = now.Add(time.Second)
	if f := c.Frames()[0]; f[0] != 255 {
		t.Fatalf("normal output not restored after sweep: ch1=%d", f[0])
	}
	if c.State().Sweep != nil {
		t.Error("sweep still reported")
	}
}

func TestManualSweepSteps(t *testing.T) {
	c, _ := newTest(t)
	steps, _ := SweepPlan{From: 1, To: 3, Value: 255, Manual: true}.Build()
	c.StartSweep(Sweep{Steps: steps, Manual: true})
	c.StepSweep(1)
	c.StepSweep(5)
	if s := c.State().Sweep; s.Index != 2 || s.Label != "channel 3 = 255" {
		t.Fatalf("got %+v", s)
	}
}

func TestTestSequence(t *testing.T) {
	c, now := newTest(t)
	c.StartTest()
	if f := c.Frames()[0]; f[0] != 255 || f[16] != 255 {
		t.Fatalf("red phase: uplight ch1=%d wash ch17=%d", f[0], f[16])
	}
	*now = now.Add(3500 * time.Millisecond)
	if s := c.State(); s.TestColor != "white" {
		t.Fatalf("phase = %q", s.TestColor)
	}
	*now = now.Add(time.Second)
	if c.State().TestRunning {
		t.Error("test did not end")
	}
}

func TestFixtureUpdateByTypeNameAndNumber(t *testing.T) {
	c, _ := newTest(t)
	spot := len(c.State().Fixtures) - 1
	err := c.UpdateFixture(FixtureUpdate{Index: spot, Values: map[string]int{"pan": 10, "Gobo": 20, "6": 300}})
	if err != nil {
		t.Fatal(err)
	}
	v := c.State().Fixtures[spot].Values
	if v[0] != 10 || v[3] != 20 || v[5] != 255 {
		t.Fatalf("values = %v", v)
	}
	if c.UpdateFixture(FixtureUpdate{Index: spot, Values: map[string]int{"zoom": 1}}) == nil {
		t.Error("expected unknown channel error")
	}
}

func TestLegacyConfigAndState(t *testing.T) {
	c, _ := newTest(t)
	s := c.State()
	if len(s.Addresses) != 6 || s.Addresses[3] != 19 || s.Names[0] != "Uplight 1" || s.FixtureMode != 6 {
		t.Fatalf("legacy fields: %+v %+v", s.Addresses, s.Names)
	}
	if len(s.Conflicts) != 4 {
		t.Errorf("default rig should report 4 overlaps, got %d", len(s.Conflicts))
	}
	err := c.UpdateConfig(ConfigUpdate{Universe: ptr(1), Addresses: []int{41, 47}, TargetIP: str("2.0.0.9")})
	if err != nil {
		t.Fatal(err)
	}
	s = c.State()
	if s.TargetIP != "2.0.0.9" || s.Universe != 1 || s.Addresses[1] != 47 || s.Addresses[2] != 13 {
		t.Fatalf("after config: %+v", s)
	}
	if len(s.Conflicts) != 0 {
		t.Errorf("uplights moved to universe 1 but conflicts remain: %+v", s.Conflicts)
	}
	if c.UpdateConfig(ConfigUpdate{TargetIP: str("2.0.0")}) == nil {
		t.Error("expected bad IP error")
	}
}

func TestPatchSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.json")
	c, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	f, _ := fixture.New("Spot", "spot-9ch", "", 0, 1)
	f.Channels[4].Name = "Strobe"
	if err := c.SetPatch([]fixture.Fixture{f, {Name: "Wash", Profile: "wash-fx", Address: 17}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPatch([]fixture.Fixture{{Name: "Bad", Profile: "spot-9ch", Address: 510}}); err == nil {
		t.Error("expected past-512 error")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	s := c2.State()
	if len(s.Fixtures) != 2 || s.Fixtures[0].Channels[4].Name != "Strobe" || len(s.Fixtures[1].Channels) != 3 {
		t.Fatalf("reloaded patch = %+v", s.Fixtures)
	}
}

func ptr(v int) *int       { return &v }
func str(v string) *string { return &v }

func TestRemotePINIsStable(t *testing.T) {
	c, _ := newTest(t)
	pin := c.RemotePIN()
	if len(pin) != 6 || c.RemotePIN() != pin {
		t.Fatalf("pin %q not stable", pin)
	}
}

func TestColorFade(t *testing.T) {
	c, now := newTest(t)
	c.SetColor([]int{6}, ColorSet{R: ptr(200), G: ptr(0), B: ptr(0), Fade: 2})
	if f := c.Frames()[0]; f[16] != 0 {
		t.Fatalf("fade start: wash red = %d, want 0", f[16])
	}
	*now = now.Add(time.Second)
	if f := c.Frames()[0]; f[16] != 100 {
		t.Fatalf("fade middle: wash red = %d, want 100", f[16])
	}
	if c.State().Fixtures[6].Values[0] != 200 {
		t.Error("state should report the fade target")
	}
	*now = now.Add(2 * time.Second)
	if f := c.Frames()[0]; f[16] != 200 {
		t.Fatalf("fade end: wash red = %d, want 200", f[16])
	}
}

func TestAmberFallsBackToRGBOnWash(t *testing.T) {
	c, _ := newTest(t)
	c.SetColor(nil, ColorSet{R: ptr(0), G: ptr(0), B: ptr(0), A: ptr(255), W: ptr(0), UV: ptr(0)})
	s := c.State()
	if v := s.Fixtures[6].Values; v[0] != 255 || v[1] != 150 || v[2] != 0 {
		t.Errorf("wash amber = %v, want 255,150,0", v)
	}
	if v := s.Fixtures[0].Values; v[0] != 0 || v[3] != 255 {
		t.Errorf("uplight should use its amber channel, got %v", v)
	}
}

func TestFlashStrobeRainbow(t *testing.T) {
	c, now := newTest(t)
	c.SetBlackout(ptrb(true))
	c.SetFlash(true)
	if f := c.Frames()[0]; f[16] != 255 || f[4] != 255 {
		t.Fatalf("flash should override blackout: wash r=%d uplight w=%d", f[16], f[4])
	}
	c.SetFlash(false)
	c.SetBlackout(ptrb(false))
	c.SetColor(nil, ColorSet{R: ptr(255), G: ptr(0), B: ptr(0)})
	c.SetStrobe(2) // 4 half-periods per second
	if f := c.Frames()[0]; f[16] != 255 {
		t.Fatal("strobe on-phase should show the color")
	}
	*now = now.Add(300 * time.Millisecond)
	if f := c.Frames()[0]; f[16] != 0 {
		t.Fatal("strobe off-phase should be dark")
	}
	c.SetStrobe(0)
	c.SetRainbow(8)
	a := c.Frames()[0]
	*now = now.Add(2 * time.Second)
	b := c.Frames()[0]
	if a[16] == b[16] && a[17] == b[17] && a[18] == b[18] {
		t.Error("rainbow did not change the wash color")
	}
	c.SetColor(nil, ColorSet{R: ptr(0), G: ptr(255), B: ptr(0)})
	if c.State().Perform.RainbowSec != 0 {
		t.Error("picking a color should stop the rainbow")
	}
}

func TestScenes(t *testing.T) {
	c, now := newTest(t)
	c.SetColor(nil, ColorSet{R: ptr(0), G: ptr(0), B: ptr(255)})
	if err := c.SaveScene(2, "Blue wash"); err != nil {
		t.Fatal(err)
	}
	c.SetColor(nil, ColorSet{R: ptr(255), G: ptr(0), B: ptr(0)})
	if err := c.RecallScene(2, 1); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(500 * time.Millisecond)
	if f := c.Frames()[0]; f[16] != 128 || f[18] != 128 {
		t.Fatalf("mid-recall wash = %d,%d,%d", f[16], f[17], f[18])
	}
	sc := c.State().Perform.Scenes
	if len(sc) != SceneSlots || !sc[2].Saved || sc[2].Name != "Blue wash" || sc[2].Color != "rgb(0,0,255)" || sc[0].Saved {
		t.Fatalf("scenes = %+v", sc)
	}
	if c.RecallScene(5, 0) == nil {
		t.Error("recalling an empty slot should fail")
	}
	c.ClearScene(2)
	if c.State().Perform.Scenes[2].Saved {
		t.Error("scene not cleared")
	}
}

func ptrb(v bool) *bool { return &v }
