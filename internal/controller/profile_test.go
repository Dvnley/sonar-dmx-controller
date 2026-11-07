package controller

import (
	"path/filepath"
	"testing"

	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

func spotPreset() fixture.Profile {
	return fixture.Profile{Name: "My Spot", Modes: []fixture.Mode{{Name: "9CH", Channels: []fixture.Channel{
		{Name: "Pan", Type: "pan", Default: 128}, {Name: "Tilt", Type: "TILT", Default: 128},
		{Name: "Dimmer", Type: "dimmer"}, {Name: "Strobe", Type: "shutter"},
		{Name: "Colors", Type: "color", Stops: []fixture.Stop{{Name: "White", Value: 0}, {Name: "Red", Value: 14}, {Name: " ", Value: 99}}},
		{Name: "Gobo", Type: "gobo"}, {Name: "Speed", Type: "speed"}, {Name: "Mode", Type: "weird"}, {Name: "", Type: "reset"},
	}}}}
}

func TestSaveProfileAppliesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	spot := -1
	for i, f := range c.State().Fixtures {
		if f.Profile == "spot-9ch" {
			spot = i
		}
	}
	// Dimmer at 200 on the old map should carry over to the new dimmer slot.
	if err := c.UpdateFixture(FixtureUpdate{Index: spot, Values: map[string]int{"dimmer": 200}}); err != nil {
		t.Fatal(err)
	}
	p, err := c.SaveProfile(spotPreset(), []int{spot})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "custom-my-spot" || !p.Custom || p.Kind != "moving" {
		t.Fatalf("saved %+v", p)
	}
	chs := p.Modes[0].Channels
	if chs[1].Type != fixture.Tilt || chs[7].Type != fixture.Generic || chs[8].Name != "Ch 9" || len(chs[4].Stops) != 2 {
		t.Fatalf("not cleaned: %+v", chs)
	}
	f := c.State().Fixtures[spot]
	if f.Profile != p.ID || f.ProfileName != "My Spot" || f.Channels[2].Name != "Dimmer" || f.Values[2] != 200 || f.Values[0] != 128 {
		t.Fatalf("not applied: %+v", f.Fixture)
	}

	// Editing the preset updates fixtures that use it.
	p.Modes[0].Channels[3].Name = "Shutter"
	if _, err := c.SaveProfile(p, nil); err != nil {
		t.Fatal(err)
	}
	if got := c.State().Fixtures[spot].Channels[3].Name; got != "Shutter" {
		t.Fatalf("edit not applied, got %q", got)
	}
	// A second preset with the same name gets its own ID.
	if p2, _ := c.SaveProfile(spotPreset(), nil); p2.ID != "custom-my-spot-2" {
		t.Fatalf("id %q", p2.ID)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := New(path, &fakeNet{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(c2.Profiles()) - len(fixture.Profiles); n != 2 {
		t.Fatalf("reloaded %d custom presets", n)
	}
	if f := c2.State().Fixtures[spot]; f.Channels[3].Name != "Shutter" || f.Kind != "moving" {
		t.Fatalf("reloaded fixture %+v", f.Fixture)
	}

	if err := c2.DeleteProfile("custom-my-spot"); err != nil {
		t.Fatal(err)
	}
	if f := c2.State().Fixtures[spot]; len(f.Channels) != 9 {
		t.Fatal("delete should leave the fixture's channels alone")
	}
}

func TestSaveProfileRejects(t *testing.T) {
	c, _ := newTest(t)
	bad := spotPreset()
	bad.Name = " "
	if _, err := c.SaveProfile(bad, nil); err == nil {
		t.Error("blank name accepted")
	}
	bad = spotPreset()
	bad.ID = "spot-9ch"
	if _, err := c.SaveProfile(bad, nil); err == nil {
		t.Error("overwrote a built-in")
	}
	if _, err := c.SaveProfile(spotPreset(), []int{99}); err == nil {
		t.Error("bad fixture index accepted")
	}
	if err := c.DeleteProfile("wash-fx"); err == nil {
		t.Error("deleted a built-in")
	}
}

func TestSetPatchUsesCustomProfile(t *testing.T) {
	c, _ := newTest(t)
	p, _ := c.SaveProfile(spotPreset(), nil)
	if err := c.SetPatch([]fixture.Fixture{{Name: "Spot 1", Profile: p.ID, Address: 17, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if f := c.State().Fixtures[0]; len(f.Channels) != 9 || f.Values[0] != 128 {
		t.Fatalf("%+v", f.Fixture)
	}
}
