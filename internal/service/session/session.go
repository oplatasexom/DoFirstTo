package session

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"dofirst/internal/domain"
)

var (
	ErrUnknownTask  = errors.New("unknown task")
	ErrAlreadyDone  = errors.New("task is already done")
	ErrOutOfOrder   = errors.New("earlier tasks are not done yet")
	ErrTimerRunning = errors.New("timer has not finished")
	ErrNoteRequired = errors.New("note is required")
)

const maxTickGap = 5 * time.Second

type Progress struct {
	// Elapsed - засчитанное время работы над задачей
	Elapsed time.Duration `json:"elapsed"`
	Done    bool          `json:"done"`
	DoneAt  time.Time     `json:"doneAt"`
	Note    string        `json:"note,omitempty"`
}

// State - сохраняемое состояние цикла
type State struct {
	// Cycle - идентификатор цикла
	Cycle      string    `json:"cycle"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Active string `json:"active,omitempty"`
	Paused bool                `json:"paused,omitempty"`
	Tasks  map[string]Progress `json:"tasks"`
}

func NewState(cycle string, now time.Time) State {
	return State{Cycle: cycle, StartedAt: now, Tasks: map[string]Progress{}}
}

type Status string

const (
	StatusDone Status = "done"
	StatusActive Status = "active"
	StatusReady Status = "ready"
	StatusLocked Status = "locked"
)

type TaskView struct {
	Task      domain.Task   `json:"task"`
	Status    Status        `json:"status"`
	Elapsed   time.Duration `json:"elapsed"`
	Remaining time.Duration `json:"remaining"`
	Note      string        `json:"note,omitempty"`
}

type Result struct {
	AllDone bool
	Entry *domain.JournalEntry
}

type Session struct {
	list     domain.TaskList
	state    State
	lastTick time.Time
}

func New(list domain.TaskList, state State) *Session {
	state.Tasks = maps.Clone(state.Tasks)
	if state.Tasks == nil {
		state.Tasks = map[string]Progress{}
	}
	s := &Session{list: list, state: state}

	if t, ok := s.task(s.state.Active); !ok || s.state.Tasks[t.ID].Done {
		s.state.Active = ""
	}
	if list.StrictOrder {
		s.state.Active = s.firstPending()
	}
	if t, ok := s.task(s.state.Active); !ok || t.Kind != domain.KindTimer {
		s.state.Paused = false
	}
	return s
}

func (s *Session) State() State {
	st := s.state
	st.Tasks = maps.Clone(s.state.Tasks)
	return st
}

func (s *Session) Done() bool {
	return s.firstPending() == ""
}

func (s *Session) Activate(id string) error {
	t, ok := s.task(id)
	if !ok {
		return fmt.Errorf("activate %q: %w", id, ErrUnknownTask)
	}
	if s.state.Tasks[t.ID].Done {
		return fmt.Errorf("activate %q: %w", id, ErrAlreadyDone)
	}
	if s.list.StrictOrder && s.firstPending() != id {
		return fmt.Errorf("activate %q: %w", id, ErrOutOfOrder)
	}
	if s.state.Active != id {
		s.state.Paused = false
	}
	s.state.Active = id
	return nil
}

func (s *Session) SetPaused(paused bool) {
	t, ok := s.task(s.state.Active)
	s.state.Paused = paused && ok && t.Kind == domain.KindTimer
}

func (s *Session) Paused() bool {
	return s.state.Paused
}

func (s *Session) Tick(now time.Time) bool {
	prev := s.lastTick
	s.lastTick = now

	t, ok := s.task(s.state.Active)
	if !ok || t.Kind != domain.KindTimer {
		return false
	}
	if s.state.Paused {
		return false
	}
	if prev.IsZero() {
		return true
	}
	delta := min(now.Sub(prev), maxTickGap)
	if delta > 0 {
		p := s.state.Tasks[t.ID]
		p.Elapsed += delta
		s.state.Tasks[t.ID] = p
	}
	return true
}

func (s *Session) Remaining(id string) time.Duration {
	t, ok := s.task(id)
	if !ok {
		return 0
	}
	return remaining(t, s.state.Tasks[id])
}

func (s *Session) Complete(id, note string, now time.Time) (Result, error) {
	t, ok := s.task(id)
	if !ok {
		return Result{}, fmt.Errorf("complete %q: %w", id, ErrUnknownTask)
	}
	p := s.state.Tasks[id]
	if p.Done {
		return Result{}, fmt.Errorf("complete %q: %w", id, ErrAlreadyDone)
	}
	if s.list.StrictOrder && s.firstPending() != id {
		return Result{}, fmt.Errorf("complete %q: %w", id, ErrOutOfOrder)
	}
	if left := remaining(t, p); left > 0 {
		return Result{}, fmt.Errorf("complete %q: %w: %s left", id, ErrTimerRunning, left.Round(time.Second))
	}
	note = strings.TrimSpace(note)
	if t.RequireNote && note == "" {
		return Result{}, fmt.Errorf("complete %q: %w", id, ErrNoteRequired)
	}

	p.Done, p.DoneAt, p.Note = true, now, note
	s.state.Tasks[id] = p
	if s.state.Active == id {
		s.state.Active = ""
		s.state.Paused = false
	}
	if s.list.StrictOrder {
		s.state.Active = s.firstPending()
	}

	res := Result{AllDone: s.Done()}
	if res.AllDone {
		s.state.FinishedAt = now
	}
	if t.Journal {
		res.Entry = &domain.JournalEntry{
			Cycle:   s.state.Cycle,
			TaskID:  t.ID,
			Title:   t.Title,
			Note:    note,
			Minutes: int(p.Elapsed.Round(time.Minute) / time.Minute),
			At:      now,
		}
	}
	return res, nil
}

func (s *Session) AllowedGroups() []string {
	var out []string
	add := func(ids []string) {
		for _, id := range ids {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	for _, t := range s.list.Tasks {
		if s.state.Tasks[t.ID].Done {
			add(t.Unlocks)
		}
	}
	if t, ok := s.task(s.state.Active); ok {
		add(t.Needs)
	}
	return out
}

// Snapshot возвращает задачи с прогрессом в порядке списка
func (s *Session) Snapshot() []TaskView {
	first := s.firstPending()
	out := make([]TaskView, 0, len(s.list.Tasks))
	for _, t := range s.list.Tasks {
		p := s.state.Tasks[t.ID]
		v := TaskView{Task: t, Elapsed: p.Elapsed, Remaining: remaining(t, p), Note: p.Note}
		switch {
		case p.Done:
			v.Status = StatusDone
		case s.state.Active == t.ID:
			v.Status = StatusActive
		case s.list.StrictOrder && first != t.ID:
			v.Status = StatusLocked
		default:
			v.Status = StatusReady
		}
		out = append(out, v)
	}
	return out
}

func (s *Session) task(id string) (domain.Task, bool) {
	if id == "" {
		return domain.Task{}, false
	}
	for _, t := range s.list.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return domain.Task{}, false
}

// firstPending возвращает первую невыполненную задачу или пустую строку
func (s *Session) firstPending() string {
	for _, t := range s.list.Tasks {
		if !s.state.Tasks[t.ID].Done {
			return t.ID
		}
	}
	return ""
}

func remaining(t domain.Task, p Progress) time.Duration {
	if t.Kind != domain.KindTimer || p.Done {
		return 0
	}
	return max(time.Duration(t.MinMinutes)*time.Minute-p.Elapsed, 0)
}
