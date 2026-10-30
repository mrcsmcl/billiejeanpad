package main

import (
	"context"
	"math/rand"
	"sync/atomic"
	"time"
)

// Presets govern the base interval range, in hours scale. The whole
// point of auto-play is that the person doubts whether they heard it;
// a sound every few minutes stops being mysterious and becomes a bot.
// Even "travesso" (the busiest) averages ~5 per workday.
var presetIntervals = map[string][2]time.Duration{
	"fantasma": {6 * time.Hour, 10 * time.Hour}, // ~1x/dia
	"discreto": {3 * time.Hour, 5 * time.Hour},  // 2-3x/dia
	"travesso": {1 * time.Hour, 2 * time.Hour},  // 4-8x/dia
}

// Timer schedules random auto-plays. Opt-in: starts disabled. Preset
// controls the cadence (fantasma/discreto/travesso). Both are toggled
// remotely via the "timer" command.
type Timer struct {
	enabled atomic.Bool
	preset  atomic.Value // string
}

// NewTimer returns a Timer with preset "discreto" and enabled=false.
func NewTimer() *Timer {
	t := &Timer{}
	t.preset.Store("discreto")
	return t
}

func (t *Timer) SetEnabled(v bool) { t.enabled.Store(v) }
func (t *Timer) Enabled() bool     { return t.enabled.Load() }

func (t *Timer) Preset() string {
	v := t.preset.Load()
	if v == nil {
		return "discreto"
	}
	s, _ := v.(string)
	if _, ok := presetIntervals[s]; !ok {
		return "discreto"
	}
	return s
}

// SetPreset validates and stores the preset name. Unknown presets are
// silently ignored (treated as a no-op) so a bad command can't break
// the schedule.
func (t *Timer) SetPreset(p string) {
	if _, ok := presetIntervals[p]; !ok {
		return
	}
	t.preset.Store(p)
}

// Loop runs forever until ctx is done. On each tick, if enabled and
// inside office hours, calls play().
func (t *Timer) Loop(ctx context.Context, play func()) {
	for {
		wait := t.nextInterval(time.Now())
		select {
		case <-time.After(wait):
			if t.enabled.Load() && inOfficeHours(time.Now()) {
				play()
			}
		case <-ctx.Done():
			return
		}
	}
}

// nextInterval returns the delay until the next tick. Pulls the base
// range from the active preset and samples uniformly. No time-of-day
// escalation at this scale — the point of auto-play is sparseness, not
// crescendo.
func (t *Timer) nextInterval(_ time.Time) time.Duration {
	rng := presetIntervals[t.Preset()]
	min, max := rng[0], rng[1]
	span := float64(max - min)
	return min + time.Duration(rand.Float64()*span)
}

// inOfficeHours gates playback to 09:00–18:00 with a lunch pause from
// 12:00 to 13:30. Keeps the thing quiet overnight and over lunch so it
// doesn't look like a cron bot.
func inOfficeHours(now time.Time) bool {
	h := now.Hour()
	m := now.Minute()
	if h < 9 || h >= 18 {
		return false
	}
	if h == 12 {
		return false
	}
	if h == 13 && m < 30 {
		return false
	}
	return true
}
