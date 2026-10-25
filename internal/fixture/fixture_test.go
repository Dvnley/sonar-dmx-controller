package fixture

import "testing"

func mustNew(t *testing.T, name, profile, mode string, uni, addr int) Fixture {
	t.Helper()
	f, err := New(name, profile, mode, uni, addr)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDefaultRigOverlaps(t *testing.T) {
	var fx []Fixture
	for i, a := range []int{1, 7, 13, 19, 25, 31} {
		fx = append(fx, mustNew(t, string(rune('A'+i)), "freedom-flex-h4-ip", "", 0, a))
	}
	fx = append(fx, mustNew(t, "Wash", "wash-fx", "", 0, 17), mustNew(t, "Spot", "spot-9ch", "", 0, 1))
	got := Overlaps(fx)
	// Spot 1-9 hits uplights A (1-6) and B (7-12); wash 17-19 hits C (13-18) and D (19-24).
	if len(got) != 4 {
		t.Fatalf("got %d conflicts: %+v", len(got), got)
	}
	fx[6].Universe = 1
	fx[7].Enabled = false
	if got := Overlaps(fx); len(got) != 0 {
		t.Fatalf("expected no conflicts, got %+v", got)
	}
}

func TestRenderScalesDimmerOrColor(t *testing.T) {
	up := mustNew(t, "Up", "freedom-flex-h4-ip", "", 0, 1)
	up.Set(Red, 200)
	up.Brightness = 50
	spot := mustNew(t, "Spot", "spot-9ch", "", 0, 20)
	spot.Set(Dimmer, 200)
	spot.Set(Gobo, 100)
	frames := Render([]Fixture{up, spot}, Output{Master: 100})
	f := frames[0]
	if f[0] != 100 {
		t.Errorf("uplight red = %d, want 100", f[0])
	}
	if f[19] != 128 || f[20] != 128 {
		t.Errorf("spot pan/tilt = %d/%d, want centered 128", f[19], f[20])
	}
	if f[22] != 100 || f[24] != 200 {
		t.Errorf("spot gobo/dimmer = %d/%d, want 100/200 (gobo not scaled)", f[22], f[24])
	}
	f = Render([]Fixture{up, spot}, Output{Master: 100, Blackout: true})[0]
	if f[0] != 0 || f[24] != 0 || f[19] != 128 {
		t.Errorf("blackout: red %d dimmer %d pan %d", f[0], f[24], f[19])
	}
}

func TestRenderHTPAndDisabled(t *testing.T) {
	a := mustNew(t, "A", "generic-rgb", "", 0, 1)
	b := mustNew(t, "B", "generic-rgb", "", 0, 1)
	a.Set(Red, 50)
	b.Set(Red, 90)
	if v := Render([]Fixture{a, b}, Output{Master: 100})[0][0]; v != 90 {
		t.Errorf("HTP = %d", v)
	}
	b.Enabled = false
	if v := Render([]Fixture{a, b}, Output{Master: 100})[0][0]; v != 50 {
		t.Errorf("disabled fixture still output, got %d", v)
	}
}

func TestValidate(t *testing.T) {
	f := mustNew(t, "Spot", "spot-9ch", "", 0, 505)
	if f.Validate() == nil {
		t.Error("expected past-512 error")
	}
	if _, err := New("x", "nope", "", 0, 1); err == nil {
		t.Error("expected unknown profile error")
	}
	m := FindMode(Profiles[2], "9ch (DIMMER ON 5)")
	if m.Channels[4].Type != Dimmer {
		t.Error("mode lookup should be case-insensitive")
	}
}
