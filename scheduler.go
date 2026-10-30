package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"billiejeanpad/sounds"
)

const (
	maxSchedules      = 8
	scheduleGraceTime = 5 * time.Minute
)

type Schedule struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // once, weekly, window
	Sound       string `json:"sound,omitempty"`
	Category    string `json:"category,omitempty"`
	Random      bool   `json:"random,omitempty"`
	At          string `json:"at,omitempty"`          // RFC3339, once
	Weekdays    []int  `json:"weekdays,omitempty"`    // 0=Sunday
	Clock       string `json:"clock,omitempty"`       // HH:MM, weekly
	WindowStart string `json:"windowStart,omitempty"` // HH:MM
	WindowEnd   string `json:"windowEnd,omitempty"`   // HH:MM
	Timezone    string `json:"timezone,omitempty"`    // IANA
	Enabled     bool   `json:"enabled"`
	NextRun     int64  `json:"nextRun,omitempty"`
	LastRun     int64  `json:"lastRun,omitempty"`
}

type Scheduler struct {
	mu        sync.Mutex
	schedules []Schedule
	wake      chan struct{}
}

func NewScheduler(schedules []Schedule) *Scheduler {
	s := &Scheduler{wake: make(chan struct{}, 1)}
	s.schedules = append([]Schedule(nil), schedules...)
	s.recalculate(time.Now())
	return s
}

func (s *Scheduler) Snapshot() []Schedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Schedule(nil), s.schedules...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].NextRun == 0 {
			return false
		}
		if out[j].NextRun == 0 {
			return true
		}
		return out[i].NextRun < out[j].NextRun
	})
	return out
}

func (s *Scheduler) Upsert(input Schedule) (Schedule, error) {
	if err := validateSchedule(&input); err != nil {
		return Schedule{}, err
	}
	if input.ID == "" {
		input.ID = newScheduleID()
	}
	input.Enabled = true
	input.NextRun = computeNextRun(input, time.Now())
	if input.NextRun == 0 {
		return Schedule{}, errors.New("schedule has no future execution")
	}

	s.mu.Lock()
	found := false
	for i := range s.schedules {
		if s.schedules[i].ID == input.ID {
			input.LastRun = s.schedules[i].LastRun
			s.schedules[i] = input
			found = true
			break
		}
	}
	if !found {
		if len(s.schedules) >= maxSchedules {
			s.mu.Unlock()
			return Schedule{}, errors.New("schedule limit reached")
		}
		s.schedules = append(s.schedules, input)
	}
	copyForSave := append([]Schedule(nil), s.schedules...)
	s.mu.Unlock()

	if err := replaceSchedules(copyForSave); err != nil {
		return Schedule{}, err
	}
	s.signalWake()
	return input, nil
}

func (s *Scheduler) Delete(id string) error {
	s.mu.Lock()
	filtered := s.schedules[:0]
	for _, item := range s.schedules {
		if item.ID != id {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == len(s.schedules) {
		s.mu.Unlock()
		return errors.New("schedule not found")
	}
	s.schedules = append([]Schedule(nil), filtered...)
	copyForSave := append([]Schedule(nil), s.schedules...)
	s.mu.Unlock()
	if err := replaceSchedules(copyForSave); err != nil {
		return err
	}
	s.signalWake()
	return nil
}

func (s *Scheduler) Loop(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			s.runDue(now)
		case <-s.wake:
			s.runDue(time.Now())
		case <-ctx.Done():
			return
		}
	}
}

func (s *Scheduler) runDue(now time.Time) {
	s.mu.Lock()
	changed := false
	var due []Schedule
	for i := range s.schedules {
		item := &s.schedules[i]
		if !item.Enabled {
			continue
		}
		if item.NextRun == 0 {
			item.NextRun = computeNextRun(*item, now)
			changed = true
		}
		if item.NextRun == 0 || now.Unix() < item.NextRun {
			continue
		}
		if now.Sub(time.Unix(item.NextRun, 0)) <= scheduleGraceTime {
			due = append(due, *item)
		}
		item.LastRun = now.Unix()
		if item.Kind == "once" {
			item.Enabled = false
			item.NextRun = 0
		} else {
			item.NextRun = computeNextRun(*item, now.Add(time.Minute))
		}
		changed = true
	}
	copyForSave := append([]Schedule(nil), s.schedules...)
	s.mu.Unlock()

	for _, item := range due {
		playScheduledSound(item)
	}
	if changed {
		if err := replaceSchedules(copyForSave); err != nil {
			logf("save schedules: %v", err)
		}
		go sendHeartbeat()
	}
}

func (s *Scheduler) recalculate(now time.Time) {
	changed := false
	for i := range s.schedules {
		if s.schedules[i].Enabled && s.schedules[i].NextRun == 0 {
			s.schedules[i].NextRun = computeNextRun(s.schedules[i], now)
			changed = true
		}
	}
	if changed {
		_ = replaceSchedules(s.schedules)
	}
}

func (s *Scheduler) signalWake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func playScheduledSound(item Schedule) {
	var sound *sounds.Sound
	switch {
	case item.Sound != "":
		sound = sounds.ByName(item.Sound)
	case item.Category != "":
		pool := sounds.ByCategory(item.Category)
		if len(pool) > 0 {
			sound = &pool[randomIndex(len(pool))]
		}
	case item.Random:
		sound = sounds.Random()
	}
	if sound == nil {
		logf("schedule %s: sound not found", item.ID)
		return
	}
	logf("schedule %s: play %s/%s", item.ID, sound.Category, sound.Name)
	playWAVAutomatically(sound.Data)
}

func validateSchedule(item *Schedule) error {
	item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
	item.Timezone = strings.TrimSpace(item.Timezone)
	if item.Timezone == "" {
		item.Timezone = "America/Sao_Paulo"
	}
	if _, err := time.LoadLocation(item.Timezone); err != nil {
		return errors.New("invalid timezone")
	}
	if item.Sound != "" && sounds.ByName(item.Sound) == nil {
		return errors.New("unknown sound")
	}
	if item.Category != "" && len(sounds.ByCategory(item.Category)) == 0 {
		return errors.New("unknown category")
	}
	if item.Sound == "" && item.Category == "" && !item.Random {
		return errors.New("schedule needs a sound, category, or random mode")
	}
	item.Weekdays = normalizeWeekdays(item.Weekdays)
	switch item.Kind {
	case "once":
		if _, err := time.Parse(time.RFC3339, item.At); err != nil {
			return errors.New("invalid one-time date")
		}
	case "weekly":
		if len(item.Weekdays) == 0 || !validClock(item.Clock) {
			return errors.New("weekly schedule needs weekdays and time")
		}
	case "window":
		if len(item.Weekdays) == 0 || !validClock(item.WindowStart) || !validClock(item.WindowEnd) {
			return errors.New("random window needs weekdays, start, and end")
		}
		if item.WindowStart >= item.WindowEnd {
			return errors.New("window end must be after start")
		}
	default:
		return errors.New("unknown schedule kind")
	}
	return nil
}

func computeNextRun(item Schedule, after time.Time) int64 {
	loc, err := time.LoadLocation(item.Timezone)
	if err != nil {
		return 0
	}
	localAfter := after.In(loc)
	if item.Kind == "once" {
		at, err := time.Parse(time.RFC3339, item.At)
		if err != nil || !at.After(after) {
			return 0
		}
		return at.Unix()
	}
	for dayOffset := 0; dayOffset <= 14; dayOffset++ {
		day := localAfter.AddDate(0, 0, dayOffset)
		if !containsWeekday(item.Weekdays, int(day.Weekday())) {
			continue
		}
		if item.Kind == "weekly" {
			candidate := atClock(day, item.Clock, loc)
			if candidate.After(localAfter) {
				return candidate.Unix()
			}
			continue
		}

		start := atClock(day, item.WindowStart, loc)
		end := atClock(day, item.WindowEnd, loc)
		if !end.After(localAfter) {
			continue
		}
		if start.Before(localAfter.Add(time.Minute)) {
			start = localAfter.Add(time.Minute)
		}
		if !start.Before(end) {
			continue
		}
		spanSeconds := int64(end.Sub(start) / time.Second)
		return start.Add(time.Duration(secureRandomInt(spanSeconds)) * time.Second).Unix()
	}
	return 0
}

func atClock(day time.Time, clock string, loc *time.Location) time.Time {
	parts := strings.Split(clock, ":")
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
}

func validClock(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	hour, errH := strconv.Atoi(value[:2])
	minute, errM := strconv.Atoi(value[3:])
	return errH == nil && errM == nil && hour >= 0 && hour < 24 && minute >= 0 && minute < 60
}

func normalizeWeekdays(days []int) []int {
	seen := map[int]bool{}
	var result []int
	for _, day := range days {
		if day >= 0 && day <= 6 && !seen[day] {
			seen[day] = true
			result = append(result, day)
		}
	}
	sort.Ints(result)
	return result
}

func containsWeekday(days []int, target int) bool {
	for _, day := range days {
		if day == target {
			return true
		}
	}
	return false
}

func newScheduleID() string {
	var bytes [8]byte
	if _, err := cryptorand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(bytes[:])
}

func secureRandomInt(max int64) int64 {
	if max <= 1 {
		return 0
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(max))
	if err != nil {
		return time.Now().UnixNano() % max
	}
	return value.Int64()
}

func randomIndex(length int) int {
	return int(secureRandomInt(int64(length)))
}
