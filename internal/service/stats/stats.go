package stats

import (
	"time"

	"dofirst/internal/domain"
)

type DaySummary struct {
	Cycle   string `json:"cycle"`
	Minutes int    `json:"minutes"`
	Done    bool   `json:"done"`
}

type Summary struct {
	Streak        int                   `json:"streak"`
	Best          int                   `json:"best"`
	DoneDays      int                   `json:"doneDays"`
	AvgRoutineMin int                   `json:"avgRoutineMin"`
	Days          []DaySummary          `json:"days"`
	Disables      []domain.DisableEvent `json:"disables"`
}

func Summarize(days []Day, disables []domain.DisableEvent, current string) Summary {
	s := Summary{
		Streak:   Streak(days, current),
		Days:     make([]DaySummary, 0, len(days)),
		Disables: append([]domain.DisableEvent{}, disables...),
	}
	run, total := 0, 0
	for _, d := range days {
		item := DaySummary{Cycle: d.Cycle, Done: d.Done()}
		if d.Done() {
			item.Minutes = int(d.DoneAt.Sub(d.LockedAt).Round(time.Minute) / time.Minute)
			total += item.Minutes
			s.DoneDays++
			run++
			s.Best = max(s.Best, run)
		} else {
			run = 0
		}
		s.Days = append(s.Days, item)
	}
	if s.DoneDays > 0 {
		s.AvgRoutineMin = (total + s.DoneDays/2) / s.DoneDays
	}
	return s
}

type Day struct {
	Cycle    string    `json:"cycle"`
	LockedAt time.Time `json:"lockedAt"`
	DoneAt   time.Time `json:"doneAt"`
}

// сообщает, были ли в этом цикле выполнены все задачи
func (d Day) Done() bool {
	return !d.DoneAt.IsZero()
}

func Streak(days []Day, current string) int {
	n := 0
	for i := len(days) - 1; i >= 0; i-- {
		switch d := days[i]; {
		case d.Done():
			n++
		case i == len(days)-1 && d.Cycle == current:
			// сегодняшний цикл еще идет
		default:
			return n
		}
	}
	return n
}
