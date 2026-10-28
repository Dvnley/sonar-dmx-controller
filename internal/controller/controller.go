// Package controller owns the show state and the 40 Hz Art-Net output loop.
package controller

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dvnley/sonar-dmx-controller/internal/artnet"
	"github.com/dvnley/sonar-dmx-controller/internal/fixture"
)

// Config is what gets saved to disk.
type Config struct {
	TargetIP string            `json:"target_ip"`
	FPS      int               `json:"fps"`
	Master   int               `json:"master"` // 0-100
	Fixtures []fixture.Fixture `json:"fixtures"`
	// RemotePIN guards the UI when it is opened from another device (the
	// iPad). It is made once and kept so the iPad stays signed in.
	RemotePIN string  `json:"remote_pin,omitempty"`
	Scenes    []Scene `json:"scenes,omitempty"`
}

// UplightProfile is the fixture the v0.2 "fixed 6-slot" API fields refer to.
const UplightProfile = "freedom-flex-h4-ip"

// DefaultConfig is the rig as it is patched today. The uplights at 1-36
// overlap the spot (1-9) and the wash (17-19); the UI warns about it until
// one group is re-patched.
func DefaultConfig() Config {
	cfg := Config{TargetIP: "2.0.0.1", FPS: 40, Master: 100}
	for i, addr := range []int{1, 7, 13, 19, 25, 31} {
		f, _ := fixture.New(fmt.Sprintf("Uplight %d", i+1), UplightProfile, "6CH", 0, addr)
		cfg.Fixtures = append(cfg.Fixtures, f)
	}
	wash, _ := fixture.New("Wash FX", "wash-fx", "3CH", 0, 17)
	spot, _ := fixture.New("Spot", "spot-9ch", "", 0, 1)
	cfg.Fixtures = append(cfg.Fixtures, wash, spot)
	return cfg
}

// Sender is the Art-Net side, an interface so tests can capture frames.
type Sender interface {
	SendDmx(target string, universe int, data []byte) error
	Poll(addrs ...string) error
	Seen(ip string, window time.Duration) bool
	Nodes() []artnet.Node
}

// Controller is safe for concurrent use.
type Controller struct {
	mu       sync.Mutex
	cfg      Config
	path     string
	blackout bool
	testFrom time.Time
	sweep    *Sweep
	sendErr  string
	saveT    *time.Timer

	fades        map[int]*fade
	flashUntil   time.Time
	strobeHz     float64
	rainbowSec   float64
	rainbowStart time.Time
	strobeStart  time.Time
	flashOnly    map[int]bool // nil: flash everything
	runs         []*ProgramRun
	live         []fixture.Fixture // last look sent, for the UI
	beats        BeatClock

	Net       Sender
	BoundPort bool
	now       func() time.Time
}

const testStep = time.Second

var testColors = []string{"red", "green", "blue", "white"}

// New loads the config at path, or starts from DefaultConfig if it is missing.
func New(path string, sender Sender) (*Controller, error) {
	c := &Controller{cfg: DefaultConfig(), path: path, Net: sender, now: time.Now, fades: map[int]*fade{}}
	if path != "" {
		data, err := os.ReadFile(path)
		switch {
		case err == nil:
			var cfg Config
			if err := json.Unmarshal(data, &cfg); err != nil {
				return nil, fmt.Errorf("read %s: %w", path, err)
			}
			c.cfg = cfg
		case !errors.Is(err, os.ErrNotExist):
			return nil, err
		}
	}
	c.normalize()
	return c, nil
}

func (c *Controller) normalize() {
	if c.cfg.FPS <= 0 || c.cfg.FPS > 44 {
		c.cfg.FPS = 40
	}
	if c.cfg.TargetIP == "" {
		c.cfg.TargetIP = "2.0.0.1"
	}
	c.cfg.Master = clamp(c.cfg.Master, 0, 100)
	for i := range c.cfg.Fixtures {
		c.cfg.Fixtures[i].Normalize()
	}
}

// changed must be called with mu held after any edit worth saving.
// Saves are debounced so dragging a fader does not hammer the disk.
func (c *Controller) changed() {
	if c.path == "" {
		return
	}
	if c.saveT != nil {
		c.saveT.Stop()
	}
	c.saveT = time.AfterFunc(300*time.Millisecond, func() { _ = c.Save() })
}

// RemotePIN returns the PIN for remote devices, creating a 6-digit one the
// first time.
func (c *Controller) RemotePIN() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.RemotePIN == "" {
		n, err := rand.Int(rand.Reader, big.NewInt(1000000))
		if err != nil {
			n = big.NewInt(time.Now().UnixNano() % 1000000)
		}
		c.cfg.RemotePIN = fmt.Sprintf("%06d", n.Int64())
		c.changed()
	}
	return c.cfg.RemotePIN
}

// Save writes the config to disk now.
func (c *Controller) Save() error {
	c.mu.Lock()
	data, err := json.MarshalIndent(c.cfg, "", "  ")
	path := c.path
	c.mu.Unlock()
	if err != nil || path == "" {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Run sends frames at the configured rate and polls for nodes until ctx ends.
func (c *Controller) Run(ctx context.Context) {
	c.mu.Lock()
	fps := c.cfg.FPS
	c.mu.Unlock()
	tick := time.NewTicker(time.Second / time.Duration(fps))
	poll := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	defer poll.Stop()
	c.Poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c.sendFrames()
		case <-poll.C:
			c.Poll()
		}
	}
}

func (c *Controller) sendFrames() {
	c.mu.Lock()
	target := c.cfg.TargetIP
	frames := c.frames()
	c.mu.Unlock()
	var errs []string
	for uni, data := range frames {
		if err := c.Net.SendDmx(target, uni, data); err != nil {
			errs = append(errs, err.Error())
		}
	}
	c.mu.Lock()
	c.sendErr = strings.Join(errs, "; ")
	c.mu.Unlock()
}

// Poll sends ArtPoll to the target and to the broadcast of its subnet.
func (c *Controller) Poll() {
	c.mu.Lock()
	target := c.cfg.TargetIP
	c.mu.Unlock()
	addrs := []string{target}
	if n := artnet.RouteFor(target, artnet.LocalIPv4s()); n != nil {
		addrs = append(addrs, artnet.Broadcast(n))
	}
	_ = c.Net.Poll(addrs...)
}

// Frames returns what would be sent right now, keyed by universe.
func (c *Controller) Frames() map[int][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frames()
}

func (c *Controller) frames() map[int][]byte {
	now := c.now()
	if c.sweep != nil {
		i, _ := c.sweep.current(now)
		if i >= 0 {
			frames := map[int][]byte{}
			for _, u := range fixture.Universes(c.cfg.Fixtures) {
				frames[u] = make([]byte, 512)
			}
			frames[c.sweep.Universe] = c.sweep.frame(i)
			return frames
		}
		c.sweep = nil
	}
	fixtures, out := c.performLook(now)
	c.live = fixtures
	if phase := c.testPhase(now); phase >= 0 {
		fixtures = testLook(fixtures, testColors[phase])
		out = fixture.Output{Master: 100}
	}
	return fixture.Render(fixtures, out)
}

func (c *Controller) testPhase(now time.Time) int {
	if c.testFrom.IsZero() {
		return -1
	}
	p := int(now.Sub(c.testFrom) / testStep)
	if p >= len(testColors) {
		c.testFrom = time.Time{}
		return -1
	}
	return p
}

// testLook copies the patch with every color fixture at full in one color.
func testLook(src []fixture.Fixture, color string) []fixture.Fixture {
	out := make([]fixture.Fixture, len(src))
	for i, f := range src {
		f.Values = append(fixture.Values(nil), f.Values...)
		f.Brightness = 100
		if f.HasColor() {
			for j, ch := range f.Channels {
				if ch.Type.IsColor() {
					f.Values[j] = 0
				}
			}
			switch color {
			case "red":
				f.Set(fixture.Red, 255)
			case "green":
				f.Set(fixture.Green, 255)
			case "blue":
				f.Set(fixture.Blue, 255)
			case "white":
				if !f.Set(fixture.White, 255) {
					f.Set(fixture.Red, 255)
					f.Set(fixture.Green, 255)
					f.Set(fixture.Blue, 255)
				}
			}
			f.Set(fixture.Dimmer, 255)
		}
		out[i] = f
	}
	return out
}

// FixtureView is a fixture as the API reports it.
type FixtureView struct {
	Index       int    `json:"index"`
	End         int    `json:"end"`
	Kind        string `json:"kind"`
	ProfileName string `json:"profile_name"`
	fixture.Fixture
}

// State is the GET /api/state body. The universe/fixture_mode/addresses/
// names fields keep the v0.2 shape and describe the uplights.
type State struct {
	TargetIP    string                `json:"target_ip"`
	FPS         int                   `json:"fps"`
	Master      int                   `json:"master"`
	Blackout    bool                  `json:"blackout"`
	TestRunning bool                  `json:"test_running"`
	TestColor   string                `json:"test_color,omitempty"`
	Universe    int                   `json:"universe"`
	FixtureMode int                   `json:"fixture_mode"`
	Addresses   []int                 `json:"addresses"`
	Names       []string              `json:"names"`
	Fixtures    []FixtureView         `json:"fixtures"`
	Conflicts   []fixture.Conflict    `json:"conflicts"`
	Sweep       *SweepStatus          `json:"sweep"`
	ConfigPath  string                `json:"config_path"`
	Types       []fixture.ChannelType `json:"channel_types"`
	Perform     PerformState          `json:"perform"`
}

// State snapshots everything the UI needs.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	s := State{
		TargetIP: c.cfg.TargetIP, FPS: c.cfg.FPS, Master: c.cfg.Master, Blackout: c.blackout,
		FixtureMode: 6, Addresses: []int{}, Names: []string{}, Fixtures: []FixtureView{},
		Conflicts: fixture.Overlaps(c.cfg.Fixtures), ConfigPath: c.path, Types: fixture.ChannelTypes,
		Perform: c.performState(),
	}
	if s.Conflicts == nil {
		s.Conflicts = []fixture.Conflict{}
	}
	if p := c.testPhase(now); p >= 0 {
		s.TestRunning, s.TestColor = true, testColors[p]
	}
	if c.sweep != nil {
		s.Sweep = c.sweep.status(now)
	}
	first := true
	for i, f := range c.cfg.Fixtures {
		f.Values = append(fixture.Values(nil), f.Values...)
		f.Channels = append([]fixture.Channel(nil), f.Channels...)
		v := FixtureView{Index: i, End: f.End(), Fixture: f, Kind: "color", ProfileName: f.Profile}
		if p, ok := fixture.FindProfile(f.Profile); ok {
			v.Kind, v.ProfileName = p.Kind, p.Name
		}
		s.Fixtures = append(s.Fixtures, v)
		if f.Profile == UplightProfile {
			if first {
				s.Universe, first = f.Universe, false
			}
			s.Addresses = append(s.Addresses, f.Address)
			s.Names = append(s.Names, f.Name)
		}
	}
	return s
}

// Diag is the GET /api/diag body.
type Diag struct {
	AN2Online  bool          `json:"an2_online"`
	ArtnetIPOK bool          `json:"artnet_ip_ok"`
	LocalIPs   []string      `json:"local_ips"`
	Target     string        `json:"target"`
	Interface  string        `json:"interface_network,omitempty"`
	BoundPort  bool          `json:"bound_port_6454"`
	Nodes      []artnet.Node `json:"nodes"`
	SendError  string        `json:"send_error,omitempty"`
}

// Diag reports whether the Mac is on the Art-Net subnet and the node answers.
func (c *Controller) Diag() Diag {
	c.mu.Lock()
	target, sendErr := c.cfg.TargetIP, c.sendErr
	c.mu.Unlock()
	locals := artnet.LocalIPv4s()
	d := Diag{
		Target: target, LocalIPs: []string{}, BoundPort: c.BoundPort, SendError: sendErr,
		AN2Online: c.Net.Seen(target, 10*time.Second), Nodes: c.Net.Nodes(),
	}
	for _, n := range locals {
		d.LocalIPs = append(d.LocalIPs, n.IP.String())
	}
	if n := artnet.RouteFor(target, locals); n != nil {
		d.ArtnetIPOK, d.Interface = true, n.String()
	}
	if d.Nodes == nil {
		d.Nodes = []artnet.Node{}
	}
	return d
}

// StartTest runs the red, green, blue, white test once over the color fixtures.
func (c *Controller) StartTest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.testFrom = c.now()
}

// SetBlackout sets blackout, or toggles it when on is nil.
func (c *Controller) SetBlackout(on *bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on == nil {
		c.blackout = !c.blackout
	} else {
		c.blackout = *on
	}
	return c.blackout
}

// SetMaster sets the grand master, 0-100.
func (c *Controller) SetMaster(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.Master = clamp(v, 0, 100)
	c.changed()
}

// ColorSet is the /api/color body. Nil fields are left unchanged.
type ColorSet struct {
	R, G, B, A, W, UV, Dimmer *int
	Fade                      float64 // seconds
}

// SetColor applies a color to the listed fixtures (nil = all), fading over
// col.Fade seconds. A full color (r, g and b all given) is mixed into RGB on
// fixtures that lack amber, white or UV emitters, so "Amber" still shows on
// the RGB-only wash. Fixtures with a color wheel are not touched. It also
// stops the rainbow effect.
func (c *Controller) SetColor(targets []int, col ColorSet) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	idx, err := c.indexes(targets)
	if err != nil {
		return err
	}
	now := c.now()
	c.rainbowSec = 0
	c.removeFromRuns(idx)
	full := col.R != nil && col.G != nil && col.B != nil
	for _, i := range idx {
		f := &c.cfg.Fixtures[i]
		if !f.HasColor() && col.Dimmer == nil {
			continue
		}
		vals := map[fixture.ChannelType]*int{
			fixture.Red: col.R, fixture.Green: col.G, fixture.Blue: col.B,
			fixture.Amber: col.A, fixture.White: col.W, fixture.UV: col.UV, fixture.Dimmer: col.Dimmer,
		}
		if full {
			r, g, b := float64(*col.R), float64(*col.G), float64(*col.B)
			mix := func(t fixture.ChannelType, v *int, mr, mg, mb float64) {
				if v != nil && *v > 0 && f.Find(t) < 0 {
					k := float64(*v) / 255
					r, g, b = r+mr*k, g+mg*k, b+mb*k
				}
			}
			mix(fixture.Amber, col.A, 255, 150, 0)
			mix(fixture.White, col.W, 255, 255, 255)
			mix(fixture.UV, col.UV, 90, 0, 255)
			ri, gi, bi := int(math.Min(r, 255)), int(math.Min(g, 255)), int(math.Min(b, 255))
			vals[fixture.Red], vals[fixture.Green], vals[fixture.Blue] = &ri, &gi, &bi
		}
		c.beginFade(i, seconds(col.Fade), now)
		for t, v := range vals {
			if v != nil {
				f.Set(t, uint8(clamp(*v, 0, 255)))
			}
		}
	}
	c.changed()
	return nil
}

// FixtureUpdate is the /api/fixture body. Values maps a channel (its 1-based
// position, its type such as "pan", or its name) to a DMX value.
type FixtureUpdate struct {
	Index      int            `json:"index"`
	Brightness *int           `json:"brightness"`
	Enabled    *bool          `json:"enabled"`
	Values     map[string]int `json:"values"`
}

// UpdateFixture changes one fixture's brightness, enabled flag or channels.
func (c *Controller) UpdateFixture(u FixtureUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if u.Index < 0 || u.Index >= len(c.cfg.Fixtures) {
		return fmt.Errorf("no fixture %d", u.Index)
	}
	f := &c.cfg.Fixtures[u.Index]
	delete(c.fades, u.Index)
	if u.Brightness != nil {
		f.Brightness = clamp(*u.Brightness, 0, 100)
	}
	if u.Enabled != nil {
		f.Enabled = *u.Enabled
	}
	for key, v := range u.Values {
		i := channelIndex(*f, key)
		if i < 0 {
			return fmt.Errorf("%s has no channel %q", f.Name, key)
		}
		f.Values[i] = uint8(clamp(v, 0, 255))
	}
	c.changed()
	return nil
}

func channelIndex(f fixture.Fixture, key string) int {
	if n, err := strconv.Atoi(key); err == nil {
		if n >= 1 && n <= len(f.Channels) {
			return n - 1
		}
		return -1
	}
	if i := f.Find(fixture.ChannelType(strings.ToLower(key))); i >= 0 {
		return i
	}
	for i, ch := range f.Channels {
		if strings.EqualFold(ch.Name, key) {
			return i
		}
	}
	return -1
}

// ResetFixture puts a fixture's values back to its channel defaults.
func (c *Controller) ResetFixture(index int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.cfg.Fixtures) {
		return fmt.Errorf("no fixture %d", index)
	}
	f := &c.cfg.Fixtures[index]
	delete(c.fades, index)
	for i, ch := range f.Channels {
		f.Values[i] = ch.Default
	}
	c.changed()
	return nil
}

// ConfigUpdate is the /api/config body. The v0.2 fields (universe,
// fixture_mode, addresses, names) apply to the uplights in patch order.
type ConfigUpdate struct {
	TargetIP    *string  `json:"target_ip"`
	FPS         *int     `json:"fps"`
	Universe    *int     `json:"universe"`
	FixtureMode *int     `json:"fixture_mode"`
	Addresses   []int    `json:"addresses"`
	Names       []string `json:"names"`
}

// UpdateConfig applies a /api/config body.
func (c *Controller) UpdateConfig(u ConfigUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if u.TargetIP != nil {
		ip := strings.TrimSpace(*u.TargetIP)
		if net.ParseIP(ip).To4() == nil {
			return fmt.Errorf("invalid target_ip %q", ip)
		}
		c.cfg.TargetIP = ip
	}
	if u.FPS != nil {
		c.cfg.FPS = clamp(*u.FPS, 1, 44)
	}
	if u.FixtureMode != nil && *u.FixtureMode != 6 {
		return fmt.Errorf("fixture_mode %d is not supported for the uplights; edit the channel map in the patch instead", *u.FixtureMode)
	}
	n := 0
	for i := range c.cfg.Fixtures {
		f := &c.cfg.Fixtures[i]
		if f.Profile != UplightProfile {
			continue
		}
		if u.Universe != nil {
			f.Universe = clamp(*u.Universe, 0, 0x7fff)
		}
		if n < len(u.Addresses) {
			f.Address = clamp(u.Addresses[n], 1, 512)
		}
		if n < len(u.Names) && strings.TrimSpace(u.Names[n]) != "" {
			f.Name = strings.TrimSpace(u.Names[n])
		}
		n++
	}
	c.changed()
	return nil
}

// SetPatch replaces the whole patch. Overlaps are allowed (and reported in
// the state); fixtures that run past channel 512 are rejected.
func (c *Controller) SetPatch(fixtures []fixture.Fixture) error {
	for i := range fixtures {
		f := &fixtures[i]
		if strings.TrimSpace(f.Name) == "" {
			f.Name = fmt.Sprintf("Fixture %d", i+1)
		}
		if len(f.Channels) == 0 {
			p, ok := fixture.FindProfile(f.Profile)
			if !ok {
				return fmt.Errorf("%s: unknown profile %q and no channels", f.Name, f.Profile)
			}
			m := fixture.FindMode(p, f.Mode)
			f.Mode, f.Channels = m.Name, append([]fixture.Channel(nil), m.Channels...)
		}
		for j := range f.Channels {
			if f.Channels[j].Type == "" {
				f.Channels[j].Type = fixture.Generic
			}
		}
		f.Normalize()
		if err := f.Validate(); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg.Fixtures = fixtures
	c.fades = map[int]*fade{}
	c.runs = nil
	c.changed()
	return nil
}

// StartSweep takes over the output with a channel sweep.
func (c *Controller) StartSweep(s Sweep) error {
	if len(s.Steps) == 0 {
		return errors.New("sweep has no steps")
	}
	if s.Universe < 0 || s.Universe > 0x7fff {
		return errors.New("universe out of range")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s.start, s.index = c.now(), 0
	c.sweep = &s
	return nil
}

// StopSweep returns to normal output.
func (c *Controller) StopSweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweep = nil
}

// StepSweep moves a manual sweep by delta steps (wrapping is not allowed).
func (c *Controller) StepSweep(delta int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sweep == nil {
		return errors.New("no sweep running")
	}
	if !c.sweep.Manual {
		return errors.New("sweep is timed; stop it and start a manual one to step")
	}
	c.sweep.index = clamp(c.sweep.index+delta, 0, len(c.sweep.Steps)-1)
	return nil
}

func (c *Controller) indexes(targets []int) ([]int, error) {
	if targets == nil {
		out := make([]int, len(c.cfg.Fixtures))
		for i := range out {
			out[i] = i
		}
		return out, nil
	}
	for _, i := range targets {
		if i < 0 || i >= len(c.cfg.Fixtures) {
			return nil, fmt.Errorf("no fixture %d", i)
		}
	}
	return targets, nil
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
