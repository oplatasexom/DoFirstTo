package schedule

import (
	"testing"
	"time"

	"dofirst/internal/domain"
)

var zone = time.FixedZone("T", 3*3600)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, zone)
}

func allDays() []time.Weekday {
	return []time.Weekday{
		time.Sunday, time.Monday, time.Tuesday, time.Wednesday,
		time.Thursday, time.Friday, time.Saturday,
	}
}

func evening() domain.Schedule {
	return domain.Schedule{Enabled: true, Bedtime: 23 * 60, WakeTime: 7 * 60, Days: allDays()}
}

func TestEvaluate(t *testing.T) {
	firstRun := at(10, 12, 0)
	noon := domain.TimeOfDay(12 * 60)
	earlyRelease := domain.TimeOfDay(2 * 60)

	tests := []struct {
		name       string
		schedule   func(*domain.Schedule)
		marks      Marks
		now        time.Time
		wantPhase  Phase
		wantReason Reason
		wantCycle  string
		wantNext   time.Time
	}{
		{
			name:       "schedule disabled",
			schedule:   func(s *domain.Schedule) { s.Enabled = false },
			now:        at(11, 9, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonScheduleOff,
		},
		{
			name:       "first day is never locked",
			marks:      Marks{FirstRun: firstRun},
			now:        at(10, 15, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonNotArmed,
			wantCycle:  "2026-11-10",
			wantNext:   at(10, 23, 0),
		},
		{
			name:       "armed after first bedtime, quiet at night by default",
			marks:      Marks{FirstRun: firstRun},
			now:        at(10, 23, 30),
			wantPhase:  PhaseFree,
			wantReason: ReasonArmed,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 7, 0),
		},
		{
			name:       "night screen when enabled",
			schedule:   func(s *domain.Schedule) { s.NightScreen = true },
			marks:      Marks{FirstRun: firstRun},
			now:        at(11, 1, 0),
			wantPhase:  PhaseNight,
			wantReason: ReasonNight,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 7, 0),
		},
		{
			name:       "night screen skipped for this night",
			schedule:   func(s *domain.Schedule) { s.NightScreen = true },
			marks:      Marks{FirstRun: firstRun, NightOffCycle: "2026-11-11"},
			now:        at(11, 1, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonArmed,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 7, 0),
		},
		{
			name:       "night screen snoozed",
			schedule:   func(s *domain.Schedule) { s.NightScreen = true },
			marks:      Marks{FirstRun: firstRun, SnoozeUntil: at(10, 23, 45)},
			now:        at(10, 23, 30),
			wantPhase:  PhaseFree,
			wantReason: ReasonSnoozed,
			wantCycle:  "2026-11-11",
			wantNext:   at(10, 23, 45),
		},
		{
			name:       "snooze expired",
			schedule:   func(s *domain.Schedule) { s.NightScreen = true },
			marks:      Marks{FirstRun: firstRun, SnoozeUntil: at(10, 23, 45)},
			now:        at(10, 23, 45),
			wantPhase:  PhaseNight,
			wantReason: ReasonNight,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 7, 0),
		},
		{
			name:       "locked exactly at wake time",
			marks:      Marks{FirstRun: firstRun},
			now:        at(11, 7, 0),
			wantPhase:  PhaseLocked,
			wantReason: ReasonTasks,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "locked without first run mark",
			now:        at(11, 9, 0),
			wantPhase:  PhaseLocked,
			wantReason: ReasonTasks,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "free after all tasks done",
			marks:      Marks{FirstRun: firstRun, DoneCycle: "2026-11-11"},
			now:        at(11, 15, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonDone,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "yesterday's completion does not carry over",
			marks:      Marks{FirstRun: firstRun, DoneCycle: "2026-11-10"},
			now:        at(11, 8, 0),
			wantPhase:  PhaseLocked,
			wantReason: ReasonTasks,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "disabled until tomorrow",
			marks:      Marks{FirstRun: firstRun, DisabledCycle: "2026-11-11"},
			now:        at(11, 8, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonDisabled,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "disabled cycle ends at next bedtime",
			marks:      Marks{FirstRun: firstRun, DisabledCycle: "2026-11-11"},
			now:        at(12, 7, 30),
			wantPhase:  PhaseLocked,
			wantReason: ReasonTasks,
			wantCycle:  "2026-11-12",
			wantNext:   at(12, 23, 0),
		},
		{
			name:       "weekday not selected",
			schedule:   func(s *domain.Schedule) { s.Days = []time.Weekday{time.Monday, time.Tuesday} },
			now:        at(11, 9, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonDayOff,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "before auto release",
			schedule:   func(s *domain.Schedule) { s.AutoRelease = &noon },
			now:        at(11, 11, 59),
			wantPhase:  PhaseLocked,
			wantReason: ReasonTasks,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 12, 0),
		},
		{
			name:       "auto release reached",
			schedule:   func(s *domain.Schedule) { s.AutoRelease = &noon },
			now:        at(11, 12, 0),
			wantPhase:  PhaseFree,
			wantReason: ReasonReleased,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "auto release before wake is capped by cycle end",
			schedule:   func(s *domain.Schedule) { s.AutoRelease = &earlyRelease },
			now:        at(11, 22, 0),
			wantPhase:  PhaseLocked,
			wantReason: ReasonTasks,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 23, 0),
		},
		{
			name:       "bedtime after midnight: still previous cycle",
			schedule:   func(s *domain.Schedule) { s.Bedtime, s.WakeTime = 1*60, 8*60 },
			marks:      Marks{DoneCycle: "2026-11-10"},
			now:        at(11, 0, 30),
			wantPhase:  PhaseFree,
			wantReason: ReasonDone,
			wantCycle:  "2026-11-10",
			wantNext:   at(11, 1, 0),
		},
		{
			name:       "bedtime after midnight: new cycle armed",
			schedule:   func(s *domain.Schedule) { s.Bedtime, s.WakeTime = 1*60, 8*60 },
			marks:      Marks{DoneCycle: "2026-11-10"},
			now:        at(11, 1, 30),
			wantPhase:  PhaseFree,
			wantReason: ReasonArmed,
			wantCycle:  "2026-11-11",
			wantNext:   at(11, 8, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := evening()
			if tt.schedule != nil {
				tt.schedule(&s)
			}
			got := Evaluate(s, tt.marks, tt.now)
			if got.Phase != tt.wantPhase || got.Reason != tt.wantReason {
				t.Errorf("phase/reason = %v/%s, want %v/%s", got.Phase, got.Reason, tt.wantPhase, tt.wantReason)
			}
			if got.Cycle != tt.wantCycle {
				t.Errorf("cycle = %q, want %q", got.Cycle, tt.wantCycle)
			}
			if !got.Next.Equal(tt.wantNext) {
				t.Errorf("next = %v, want %v", got.Next, tt.wantNext)
			}
		})
	}
}

func TestPhaseString(t *testing.T) {
	tests := map[Phase]string{PhaseFree: "free", PhaseNight: "night", PhaseLocked: "locked", Phase(42): "unknown"}
	for phase, want := range tests {
		if got := phase.String(); got != want {
			t.Errorf("Phase(%d).String() = %q, want %q", int(phase), got, want)
		}
	}
}
