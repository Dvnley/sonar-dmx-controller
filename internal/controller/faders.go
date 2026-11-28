package controller

import (
	"fmt"
	"sort"
)

// Hand faders: set any DMX channel 1-512 by hand, like a hardware desk. A
// fader that has been moved overrides whatever the app would send on that
// channel until it is handed back. Blackout still turns everything off.

// FaderState is the faders part of GET /api/state.
type FaderState struct {
	Universe int         `json:"universe"`
	Set      map[int]int `json:"set"`    // channel (1-512) -> value, only the faders moved by hand
	Output   []int       `json:"output"` // what is going out on the universe now, 512 values
}

// SetFader sets channel (1-512) on universe to value (0-255) by hand.
func (c *Controller) SetFader(universe, channel, value int) error {
	if channel < 1 || channel > 512 {
		return fmt.Errorf("channel %d is not 1-512", channel)
	}
	if universe < 0 || universe > 0x7fff {
		return fmt.Errorf("universe %d out of range", universe)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.manual == nil {
		c.manual = map[int]map[int]uint8{}
	}
	if c.manual[universe] == nil {
		c.manual[universe] = map[int]uint8{}
	}
	c.manual[universe][channel] = uint8(clamp(value, 0, 255))
	return nil
}

// SetFaders sets several channels at once (channel -> value).
func (c *Controller) SetFaders(universe int, values map[int]int) error {
	chans := make([]int, 0, len(values))
	for ch := range values {
		chans = append(chans, ch)
	}
	sort.Ints(chans)
	for _, ch := range chans {
		if err := c.SetFader(universe, ch, values[ch]); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseFaders hands channels back to the app (none listed = all of them).
func (c *Controller) ReleaseFaders(universe int, channels []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(channels) == 0 {
		delete(c.manual, universe)
		return
	}
	for _, ch := range channels {
		delete(c.manual[universe], ch)
	}
}

// applyManual must be called with mu held.
func (c *Controller) applyManual(frames map[int][]byte) {
	if c.blackout {
		return
	}
	for uni, set := range c.manual {
		if len(set) == 0 {
			continue
		}
		f := frames[uni]
		if f == nil {
			f = make([]byte, 512)
			frames[uni] = f
		}
		for ch, v := range set {
			f[ch-1] = v
		}
	}
}

// faderState must be called with mu held.
func (c *Controller) faderState() FaderState {
	s := FaderState{Set: map[int]int{}, Output: make([]int, 512)}
	for ch, v := range c.manual[0] {
		s.Set[ch] = int(v)
	}
	if f := c.frames()[0]; f != nil {
		for i, v := range f {
			s.Output[i] = int(v)
		}
	}
	return s
}
