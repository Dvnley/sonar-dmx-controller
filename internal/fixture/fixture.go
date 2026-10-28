// Package fixture holds fixture profiles, the patch and DMX frame rendering.
package fixture

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ChannelType says what a DMX channel controls. The UI uses it to pick a
// control, and the renderer uses it to apply master/brightness/blackout.
type ChannelType string

const (
	Red     ChannelType = "red"
	Green   ChannelType = "green"
	Blue    ChannelType = "blue"
	Amber   ChannelType = "amber"
	White   ChannelType = "white"
	UV      ChannelType = "uv"
	Dimmer  ChannelType = "dimmer"
	Pan     ChannelType = "pan"
	Tilt    ChannelType = "tilt"
	Shutter ChannelType = "shutter" // shutter / strobe
	Color   ChannelType = "color"   // color wheel
	Gobo    ChannelType = "gobo"
	Speed   ChannelType = "speed" // pan/tilt speed
	Auto    ChannelType = "auto"  // auto / sound programs
	Reset   ChannelType = "reset"
	Generic ChannelType = "generic"
)

// ChannelTypes lists every type, in the order the patch editor offers them.
var ChannelTypes = []ChannelType{Red, Green, Blue, Amber, White, UV, Dimmer, Pan, Tilt, Shutter, Color, Gobo, Speed, Auto, Reset, Generic}

// IsColor reports whether t is an LED color emitter channel.
func (t ChannelType) IsColor() bool {
	switch t {
	case Red, Green, Blue, Amber, White, UV:
		return true
	}
	return false
}

// Channel is one slot of a fixture's channel map.
type Channel struct {
	Name    string      `json:"name"`
	Type    ChannelType `json:"type"`
	Default uint8       `json:"default"`
}

// Mode is one DMX personality of a profile.
type Mode struct {
	Name     string    `json:"name"`
	Channels []Channel `json:"channels"`
}

// Profile describes a fixture model.
type Profile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Kind  string `json:"kind"` // "color" or "moving"
	Notes string `json:"notes,omitempty"`
	Modes []Mode `json:"modes"`
}

func ch(name string, t ChannelType, def uint8) Channel {
	return Channel{Name: name, Type: t, Default: def}
}

// Profiles are the built-in fixture types. Every patched fixture gets its
// own copy of the channel map, so a wrong guess can be fixed in the patch
// editor without touching this list.
var Profiles = []Profile{
	{
		ID: "freedom-flex-h4-ip", Name: "Chauvet Freedom Flex H4 IP", Kind: "color",
		Notes: "RGBAW+UV battery uplight. 6CH mode as used by v0.2.",
		Modes: []Mode{{Name: "6CH", Channels: []Channel{
			ch("Red", Red, 0), ch("Green", Green, 0), ch("Blue", Blue, 0),
			ch("Amber", Amber, 0), ch("White", White, 0), ch("UV", UV, 0),
		}}},
	},
	{
		ID: "wash-fx", Name: "Chauvet Wash FX", Kind: "color",
		Notes: "3CH RGB inferred from the red test turning the wash blue. Check the mode on the fixture's display.",
		Modes: []Mode{{Name: "3CH", Channels: []Channel{
			ch("Red", Red, 0), ch("Green", Green, 0), ch("Blue", Blue, 0),
		}}},
	},
	{
		ID: "spot-9ch", Name: "Generic moving-head spot", Kind: "moving",
		Notes: "Unconfirmed layout. Run the channel sweep and fix the channel map in the patch editor.",
		Modes: []Mode{
			{Name: "9CH (dimmer on 6)", Channels: []Channel{
				ch("Pan", Pan, 128), ch("Tilt", Tilt, 128), ch("Color wheel", Color, 0),
				ch("Gobo", Gobo, 0), ch("Shutter/strobe", Shutter, 0), ch("Dimmer", Dimmer, 0),
				ch("Pan/tilt speed", Speed, 0), ch("Auto/sound", Auto, 0), ch("Reset", Reset, 0),
			}},
			{Name: "9CH (dimmer on 5)", Channels: []Channel{
				ch("Pan", Pan, 128), ch("Tilt", Tilt, 128), ch("Color wheel", Color, 0),
				ch("Gobo", Gobo, 0), ch("Dimmer", Dimmer, 0), ch("Shutter/strobe", Shutter, 0),
				ch("Pan/tilt speed", Speed, 0), ch("Auto/sound", Auto, 0), ch("Reset", Reset, 0),
			}},
		},
	},
	{
		ID: "generic-rgb", Name: "Generic RGB", Kind: "color",
		Modes: []Mode{{Name: "3CH", Channels: []Channel{ch("Red", Red, 0), ch("Green", Green, 0), ch("Blue", Blue, 0)}}},
	},
	{
		ID: "generic-dimmer", Name: "Generic dimmer", Kind: "color",
		Modes: []Mode{{Name: "1CH", Channels: []Channel{ch("Dimmer", Dimmer, 0)}}},
	},
}

// FindProfile looks up a profile by ID.
func FindProfile(id string) (Profile, bool) {
	for _, p := range Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

// FindMode looks up a mode of a profile by name, defaulting to the first.
func FindMode(p Profile, name string) Mode {
	for _, m := range p.Modes {
		if strings.EqualFold(m.Name, name) {
			return m
		}
	}
	return p.Modes[0]
}

// Fixture is one patched fixture with its current values.
type Fixture struct {
	Name       string    `json:"name"`
	Profile    string    `json:"profile"`
	Mode       string    `json:"mode"`
	Universe   int       `json:"universe"`
	Address    int       `json:"address"` // 1-based DMX start address
	Enabled    bool      `json:"enabled"`
	Brightness int       `json:"brightness"` // 0-100
	Channels   []Channel `json:"channels"`
	Values     Values    `json:"values"`
	Pos        *Point    `json:"pos,omitempty"` // where it sits on the stage map
}

// Point is a position on the stage map, 0-1 across and down.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Values are a fixture's DMX values. They marshal as a JSON array of
// numbers rather than the base64 string Go uses for []byte.
type Values []uint8

func (v Values) MarshalJSON() ([]byte, error) {
	ints := make([]int, len(v))
	for i, b := range v {
		ints[i] = int(b)
	}
	return json.Marshal(ints)
}

func (v *Values) UnmarshalJSON(data []byte) error {
	var ints []int
	if err := json.Unmarshal(data, &ints); err != nil {
		return err
	}
	*v = make(Values, len(ints))
	for i, n := range ints {
		(*v)[i] = uint8(clamp(n, 0, 255))
	}
	return nil
}

// New patches a fixture from a profile with default values.
func New(name, profileID, mode string, universe, address int) (Fixture, error) {
	p, ok := FindProfile(profileID)
	if !ok {
		return Fixture{}, fmt.Errorf("unknown profile %q", profileID)
	}
	m := FindMode(p, mode)
	f := Fixture{
		Name: name, Profile: p.ID, Mode: m.Name, Universe: universe, Address: address,
		Enabled: true, Brightness: 100,
		Channels: append([]Channel(nil), m.Channels...),
	}
	f.Normalize()
	return f, nil
}

// Normalize makes Values match Channels and clamps the numeric fields.
func (f *Fixture) Normalize() {
	if len(f.Values) != len(f.Channels) {
		vals := make(Values, len(f.Channels))
		for i, c := range f.Channels {
			if i < len(f.Values) {
				vals[i] = f.Values[i]
			} else {
				vals[i] = c.Default
			}
		}
		f.Values = vals
	}
	f.Brightness = clamp(f.Brightness, 0, 100)
	f.Universe = clamp(f.Universe, 0, 0x7fff)
	f.Address = clamp(f.Address, 1, 512)
}

// End is the last DMX address the fixture occupies.
func (f Fixture) End() int { return f.Address + len(f.Channels) - 1 }

// Validate reports patch errors that make a fixture unusable.
func (f Fixture) Validate() error {
	if len(f.Channels) == 0 {
		return fmt.Errorf("%s: has no channels", f.Name)
	}
	if f.End() > 512 {
		return fmt.Errorf("%s: address %d with %d channels runs past 512", f.Name, f.Address, len(f.Channels))
	}
	return nil
}

// Find returns the index of the first channel of type t, or -1.
func (f Fixture) Find(t ChannelType) int {
	for i, c := range f.Channels {
		if c.Type == t {
			return i
		}
	}
	return -1
}

// Set writes a value to every channel of type t. It returns false if the
// fixture has no such channel.
func (f *Fixture) Set(t ChannelType, v uint8) bool {
	found := false
	for i, c := range f.Channels {
		if c.Type == t {
			f.Values[i] = v
			found = true
		}
	}
	return found
}

// HasColor reports whether the fixture mixes color with LED channels.
func (f Fixture) HasColor() bool {
	for _, c := range f.Channels {
		if c.Type.IsColor() {
			return true
		}
	}
	return false
}

// Conflict is a pair of enabled fixtures that share DMX channels.
type Conflict struct {
	Universe int    `json:"universe"`
	A        int    `json:"a"`
	B        int    `json:"b"`
	NameA    string `json:"name_a"`
	NameB    string `json:"name_b"`
	From     int    `json:"from"`
	To       int    `json:"to"`
	Message  string `json:"message"`
}

// Overlaps finds every pair of enabled fixtures whose addresses collide.
func Overlaps(fixtures []Fixture) []Conflict {
	var out []Conflict
	for i := 0; i < len(fixtures); i++ {
		a := fixtures[i]
		if !a.Enabled || len(a.Channels) == 0 {
			continue
		}
		for j := i + 1; j < len(fixtures); j++ {
			b := fixtures[j]
			if !b.Enabled || len(b.Channels) == 0 || a.Universe != b.Universe {
				continue
			}
			from, to := max(a.Address, b.Address), min(a.End(), b.End())
			if from > to {
				continue
			}
			rng := fmt.Sprintf("channel %d", from)
			if to > from {
				rng = fmt.Sprintf("channels %d-%d", from, to)
			}
			out = append(out, Conflict{
				Universe: a.Universe, A: i, B: j, NameA: a.Name, NameB: b.Name, From: from, To: to,
				Message: fmt.Sprintf("%s and %s share %s on universe %d", a.Name, b.Name, rng, a.Universe),
			})
		}
	}
	return out
}

// Output controls how values are scaled when rendering.
type Output struct {
	Master   int // 0-100
	Blackout bool
}

// Render builds a 512-byte DMX frame for each universe that has an enabled
// fixture. Where fixtures overlap, the highest value wins (HTP).
//
// Intensity: if a fixture has a dimmer channel, master and brightness scale
// the dimmer only; otherwise they scale its color channels.
func Render(fixtures []Fixture, out Output) map[int][]byte {
	frames := map[int][]byte{}
	for _, f := range fixtures {
		if _, ok := frames[f.Universe]; !ok {
			frames[f.Universe] = make([]byte, 512)
		}
		if !f.Enabled {
			continue
		}
		frame := frames[f.Universe]
		scale := float64(out.Master) / 100 * float64(f.Brightness) / 100
		if out.Blackout {
			scale = 0
		}
		hasDimmer := f.Find(Dimmer) >= 0
		for i, c := range f.Channels {
			addr := f.Address - 1 + i
			if addr < 0 || addr >= 512 || i >= len(f.Values) {
				continue
			}
			v := f.Values[i]
			if c.Type == Dimmer || (!hasDimmer && c.Type.IsColor()) {
				v = uint8(float64(v)*scale + 0.5)
			}
			if v > frame[addr] {
				frame[addr] = v
			}
		}
	}
	return frames
}

// Universes returns the sorted list of universes used by the patch.
func Universes(fixtures []Fixture) []int {
	seen := map[int]bool{}
	var out []int
	for _, f := range fixtures {
		if !seen[f.Universe] {
			seen[f.Universe] = true
			out = append(out, f.Universe)
		}
	}
	sort.Ints(out)
	return out
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
