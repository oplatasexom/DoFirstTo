package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const minutesPerDay = 24 * 60

type TimeOfDay int

func NewTimeOfDay(hour, minute int) (TimeOfDay, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("time of day %d:%d: out of range", hour, minute)
	}
	return TimeOfDay(hour*60 + minute), nil
}

func ParseTimeOfDay(s string) (TimeOfDay, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("time of day %q: want HH:MM", s)
	}
	hour, err := strconv.Atoi(h)
	if err != nil {
		return 0, fmt.Errorf("time of day %q: hours: %w", s, err)
	}
	minute, err := strconv.Atoi(m)
	if err != nil {
		return 0, fmt.Errorf("time of day %q: minutes: %w", s, err)
	}
	return NewTimeOfDay(hour, minute)
}

func (t TimeOfDay) Valid() bool {
	return t >= 0 && t < minutesPerDay
}

func (t TimeOfDay) String() string {
	return fmt.Sprintf("%02d:%02d", int(t)/60, int(t)%60)
}

func (t TimeOfDay) On(day time.Time) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, int(t)/60, int(t)%60, 0, 0, day.Location())
}

func (t TimeOfDay) MarshalText() ([]byte, error) {
	if !t.Valid() {
		return nil, fmt.Errorf("time of day %d: out of range", int(t))
	}
	return []byte(t.String()), nil
}

func (t *TimeOfDay) UnmarshalText(b []byte) error {
	v, err := ParseTimeOfDay(string(b))
	if err != nil {
		return err
	}
	*t = v
	return nil
}
