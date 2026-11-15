package stats

import (
	"testing"
	"time"

	"dofirst/internal/domain"
)

func TestSummarize(t *testing.T) {
	at := time.Date(2026, 11, 11, 7, 0, 0, 0, time.UTC)
	day := func(cycle string, minutes int) Day {
		d := Day{Cycle: cycle, LockedAt: at}
		if minutes > 0 {
			d.DoneAt = at.Add(time.Duration(minutes) * time.Minute)
		}
		return d
	}
	events := []domain.DisableEvent{{Cycle: "07", Mode: domain.DisableUntilTomorrow, Reason: "встреча", At: at}}

	tests := []struct {
		name string
		days []Day
		want Summary
	}{
		{
			name: "empty history",
			want: Summary{},
		},
		{
			name: "best streak is the longest run, not the current one",
			days: []Day{day("05", 60), day("06", 80), day("07", 0), day("08", 71), day("09", 0)},
			want: Summary{Streak: 0, Best: 2, DoneDays: 3, AvgRoutineMin: 70},
		},
		{
			name: "current streak with today in progress",
			days: []Day{day("08", 90), day("09", 61), day("10", 0)},
			want: Summary{Streak: 2, Best: 2, DoneDays: 2, AvgRoutineMin: 76},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := ""
			if n := len(tt.days); n > 0 && tt.name == "current streak with today in progress" {
				current = tt.days[n-1].Cycle
			}
			got := Summarize(tt.days, events, current)
			if got.Streak != tt.want.Streak || got.Best != tt.want.Best ||
				got.DoneDays != tt.want.DoneDays || got.AvgRoutineMin != tt.want.AvgRoutineMin {
				t.Errorf("Summarize = streak %d, best %d, done %d, avg %d; want %d, %d, %d, %d",
					got.Streak, got.Best, got.DoneDays, got.AvgRoutineMin,
					tt.want.Streak, tt.want.Best, tt.want.DoneDays, tt.want.AvgRoutineMin)
			}
			if len(got.Days) != len(tt.days) || len(got.Disables) != 1 {
				t.Errorf("days = %d, disables = %d; want %d, 1", len(got.Days), len(got.Disables), len(tt.days))
			}
			for i, d := range got.Days {
				if d.Done != tt.days[i].Done() {
					t.Errorf("day %s: done = %v, want %v", d.Cycle, d.Done, tt.days[i].Done())
				}
			}
		})
	}
}

func TestStreak(t *testing.T) {
	at := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	done := func(cycle string) Day { return Day{Cycle: cycle, LockedAt: at, DoneAt: at.Add(time.Hour)} }
	open := func(cycle string) Day { return Day{Cycle: cycle, LockedAt: at} }

	tests := []struct {
		name    string
		days    []Day
		current string
		want    int
	}{
		{name: "no history", want: 0},
		{name: "all done", days: []Day{done("09"), done("10"), done("11")}, current: "11", want: 3},
		{name: "today in progress keeps the streak", days: []Day{done("09"), done("10"), open("11")}, current: "11", want: 2},
		{name: "missed day breaks the streak", days: []Day{done("08"), open("09"), done("10"), done("11")}, current: "11", want: 2},
		{name: "yesterday missed", days: []Day{done("09"), open("10"), open("11")}, current: "11", want: 0},
		{name: "unfinished day is not current any more", days: []Day{done("09"), done("10"), open("11")}, current: "12", want: 0},
		{name: "first day in progress", days: []Day{open("11")}, current: "11", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Streak(tt.days, tt.current); got != tt.want {
				t.Errorf("Streak = %d, want %d", got, tt.want)
			}
		})
	}
}
