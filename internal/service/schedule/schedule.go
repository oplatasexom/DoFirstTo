package schedule

import (
	"slices"
	"time"

	"dofirst/internal/domain"
)

const CycleLayout = "2006-01-02"

type Phase int

const (
	PhaseFree Phase = iota
	PhaseNight
	PhaseLocked
)

func (p Phase) String() string {
	switch p {
	case PhaseFree:
		return "free"
	case PhaseNight:
		return "night"
	case PhaseLocked:
		return "locked"
	default:
		return "unknown"
	}
}

type Reason string

const (
	ReasonScheduleOff Reason = "schedule-off" // расписание выключено
	ReasonNotArmed    Reason = "not-armed"    // первый запуск
	ReasonDayOff      Reason = "day-off"      // день недели не отмечен
	ReasonArmed       Reason = "armed"        // отбой прошел
	ReasonSnoozed     Reason = "snoozed"      // ночной экран отложен
	ReasonNight       Reason = "night"        // действует ночной экран
	ReasonTasks       Reason = "tasks"        // задачи ещё не сделаны
	ReasonDone        Reason = "done"         // все задачи цикла выполнены
	ReasonDisabled    Reason = "disabled"     // пользователь выключил до завтра
	ReasonReleased    Reason = "released"     // сработало автоснятие
)

type Marks struct {
	FirstRun time.Time `json:"firstRun"`
	DoneCycle string `json:"doneCycle,omitempty"`
	DisabledCycle string `json:"disabledCycle,omitempty"`
	NightOffCycle string `json:"nightOffCycle,omitempty"`
	SnoozeUntil time.Time `json:"snoozeUntil"`
}

type Status struct {
	Phase  Phase
	Reason Reason
	Cycle string
	Next time.Time
}

func Evaluate(s domain.Schedule, m Marks, now time.Time) Status {
	if !s.Enabled {
		return Status{Phase: PhaseFree, Reason: ReasonScheduleOff}
	}

	day := cycleDay(s, now)
	bed := bedtimeOf(s, day)
	wake := s.WakeTime.On(day)
	end := bedtimeOf(s, day.AddDate(0, 0, 1))
	id := day.Format(CycleLayout)

	free := func(r Reason, next time.Time) Status {
		return Status{Phase: PhaseFree, Reason: r, Cycle: id, Next: next}
	}

	switch {
	case !m.FirstRun.IsZero() && bed.Before(m.FirstRun):
		return free(ReasonNotArmed, end)
	case !slices.Contains(s.Days, day.Weekday()):
		return free(ReasonDayOff, end)
	case m.DoneCycle == id:
		return free(ReasonDone, end)
	case m.DisabledCycle == id:
		return free(ReasonDisabled, end)
	}

	if now.Before(wake) {
		switch {
		case !s.NightScreen || m.NightOffCycle == id:
			return free(ReasonArmed, wake)
		case now.Before(m.SnoozeUntil):
			return free(ReasonSnoozed, earliest(m.SnoozeUntil, wake))
		default:
			return Status{Phase: PhaseNight, Reason: ReasonNight, Cycle: id, Next: wake}
		}
	}

	next := end
	if s.AutoRelease != nil {
		release := releaseOf(*s.AutoRelease, wake, end)
		if !now.Before(release) {
			return free(ReasonReleased, end)
		}
		next = release
	}
	return Status{Phase: PhaseLocked, Reason: ReasonTasks, Cycle: id, Next: next}
}

func cycleDay(s domain.Schedule, now time.Time) time.Time {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	for offset := 1; offset >= 0; offset-- {
		day := today.AddDate(0, 0, offset)
		if !bedtimeOf(s, day).After(now) {
			return day
		}
	}
	return today.AddDate(0, 0, -1)
}

func bedtimeOf(s domain.Schedule, day time.Time) time.Time {
	if s.Bedtime > s.WakeTime {
		return s.Bedtime.On(day.AddDate(0, 0, -1))
	}
	return s.Bedtime.On(day)
}

func releaseOf(at domain.TimeOfDay, wake, end time.Time) time.Time {
	release := at.On(wake)
	if !release.After(wake) {
		release = at.On(wake.AddDate(0, 0, 1))
	}
	return earliest(release, end)
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
