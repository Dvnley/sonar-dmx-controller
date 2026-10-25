package controller

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Step is one look of a channel sweep: the listed channels are set and every
// other channel in the universe is 0.
type Step struct {
	Seconds float64       `json:"seconds"`
	Values  map[int]uint8 `json:"values"` // 1-based channel -> value
	Label   string        `json:"label"`
}

// Sweep takes over the output of one universe while it runs, like the
// standalone probe script did, so there is only ever one sender.
type Sweep struct {
	Universe int    `json:"universe"`
	Manual   bool   `json:"manual"` // step with Next/Prev instead of timers
	Steps    []Step `json:"steps"`

	start time.Time
	index int
}

// SweepStatus is what the UI shows while a sweep runs.
type SweepStatus struct {
	Universe  int           `json:"universe"`
	Manual    bool          `json:"manual"`
	Index     int           `json:"index"`
	Count     int           `json:"count"`
	Label     string        `json:"label"`
	Remaining float64       `json:"remaining_s"`
	Values    map[int]uint8 `json:"values"`
	Next      string        `json:"next,omitempty"`
}

// current returns the active step index and how long it has left, or -1
// when a timed sweep has finished.
func (s *Sweep) current(now time.Time) (int, float64) {
	if len(s.Steps) == 0 {
		return -1, 0
	}
	if s.Manual {
		return s.index, 0
	}
	elapsed := now.Sub(s.start).Seconds()
	for i, st := range s.Steps {
		if elapsed < st.Seconds {
			return i, st.Seconds - elapsed
		}
		elapsed -= st.Seconds
	}
	return -1, 0
}

func (s *Sweep) frame(i int) []byte {
	frame := make([]byte, 512)
	for ch, v := range s.Steps[i].Values {
		if ch >= 1 && ch <= 512 {
			frame[ch-1] = v
		}
	}
	return frame
}

func (s *Sweep) status(now time.Time) *SweepStatus {
	i, rem := s.current(now)
	if i < 0 {
		return nil
	}
	st := &SweepStatus{
		Universe: s.Universe, Manual: s.Manual, Index: i, Count: len(s.Steps),
		Label: s.Steps[i].Label, Remaining: rem, Values: s.Steps[i].Values,
	}
	if i+1 < len(s.Steps) {
		st.Next = s.Steps[i+1].Label
	}
	return st
}

// ParseScript reads the probe script format:
//
//	"seconds:ch=val,ch=val;seconds:ch=val;2:"
//
// An empty channel list is an all-zero step.
func ParseScript(script string) ([]Step, error) {
	var steps []Step
	for n, raw := range strings.Split(script, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		secsText, chans, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, fmt.Errorf("step %d %q: expected seconds:ch=val,...", n+1, raw)
		}
		secs, err := strconv.ParseFloat(strings.TrimSpace(secsText), 64)
		if err != nil || secs < 0 {
			return nil, fmt.Errorf("step %d: bad seconds %q", n+1, secsText)
		}
		vals, err := ParseValues(chans)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", n+1, err)
		}
		steps = append(steps, Step{Seconds: secs, Values: vals, Label: Label(vals)})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("script has no steps")
	}
	return steps, nil
}

// ParseValues reads "ch=val,ch=val" with 1-based channels.
func ParseValues(s string) (map[int]uint8, error) {
	vals := map[int]uint8{}
	for _, kv := range strings.Split(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		c, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("%q: expected ch=val", kv)
		}
		chn, err := strconv.Atoi(strings.TrimSpace(c))
		if err != nil || chn < 1 || chn > 512 {
			return nil, fmt.Errorf("%q: channel must be 1-512", kv)
		}
		val, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || val < 0 || val > 255 {
			return nil, fmt.Errorf("%q: value must be 0-255", kv)
		}
		vals[chn] = uint8(val)
	}
	return vals, nil
}

// Label renders values back into "ch=val" form, sorted by channel.
func Label(vals map[int]uint8) string {
	if len(vals) == 0 {
		return "all zero"
	}
	keys := make([]int, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%d=%d", k, vals[k])
	}
	return strings.Join(parts, ",")
}

// SweepPlan is the "one channel at a time" builder from the sweep page.
type SweepPlan struct {
	From   int     `json:"from"`
	To     int     `json:"to"`
	Value  int     `json:"value"`
	Hold   float64 `json:"hold"`
	Gap    float64 `json:"gap"`
	Base   string  `json:"base"` // channels held in every step, e.g. "1=128,2=128,6=255"
	Manual bool    `json:"manual"`
}

// Build turns a plan into steps. Timed sweeps start with a blank (base only)
// step and put a gap after each channel, matching the original probe runs.
func (p SweepPlan) Build() ([]Step, error) {
	if p.From < 1 || p.To > 512 || p.From > p.To {
		return nil, fmt.Errorf("channel range must be within 1-512")
	}
	if p.Value < 0 || p.Value > 255 {
		return nil, fmt.Errorf("value must be 0-255")
	}
	base, err := ParseValues(p.Base)
	if err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	if !p.Manual && p.Hold <= 0 {
		return nil, fmt.Errorf("hold must be more than 0 seconds")
	}
	with := func(extra map[int]uint8) map[int]uint8 {
		m := map[int]uint8{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	var steps []Step
	if !p.Manual && p.Gap > 0 {
		steps = append(steps, Step{Seconds: p.Gap, Values: with(nil), Label: "start: " + Label(base)})
	}
	for chn := p.From; chn <= p.To; chn++ {
		steps = append(steps, Step{
			Seconds: p.Hold, Values: with(map[int]uint8{chn: uint8(p.Value)}),
			Label: fmt.Sprintf("channel %d = %d", chn, p.Value),
		})
		if !p.Manual && p.Gap > 0 {
			steps = append(steps, Step{Seconds: p.Gap, Values: with(nil), Label: fmt.Sprintf("gap after channel %d", chn)})
		}
	}
	return steps, nil
}
