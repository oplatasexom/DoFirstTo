package session

import (
	"errors"
	"slices"
	"testing"
	"time"

	"dofirst/internal/domain"
)

var t0 = time.Date(2026, 11, 11, 7, 5, 0, 0, time.UTC)

func tasks(strict bool) domain.TaskList {
	return domain.TaskList{
		StrictOrder: strict,
		Tasks: []domain.Task{
			{ID: "teeth", Title: "Почистить зубы", Kind: domain.KindCheck, Unlocks: []string{"music"}},
			{
				ID: "study", Title: "Учиться по roadmap", Kind: domain.KindTimer, MinMinutes: 60,
				RequireNote: true, Journal: true,
				Needs: []string{"ide", "web"}, Unlocks: []string{"web", "music"},
			},
			{ID: "plan", Title: "План на день", Kind: domain.KindCheck},
		},
	}
}

func stateWith(active string, progress map[string]Progress) State {
	st := NewState("2026-11-11", t0)
	st.Active = active
	for id, p := range progress {
		st.Tasks[id] = p
	}
	return st
}

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		strict     bool
		state      State
		wantActive string
	}{
		{name: "strict order starts with first task", strict: true, state: NewState("c", t0), wantActive: "teeth"},
		{name: "free order starts with nothing active", strict: false, state: NewState("c", t0), wantActive: ""},
		{name: "free order keeps saved active task", strict: false, state: stateWith("study", nil), wantActive: "study"},
		{
			name:       "strict order skips done tasks",
			strict:     true,
			state:      stateWith("", map[string]Progress{"teeth": {Done: true}}),
			wantActive: "study",
		},
		{name: "removed task is dropped", strict: false, state: stateWith("ghost", nil), wantActive: ""},
		{
			name:       "done task cannot stay active",
			strict:     false,
			state:      stateWith("teeth", map[string]Progress{"teeth": {Done: true}}),
			wantActive: "",
		},
		{name: "zero state is usable", strict: true, state: State{}, wantActive: "teeth"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tasks(tt.strict), tt.state)
			if got := s.State().Active; got != tt.wantActive {
				t.Errorf("Active = %q, want %q", got, tt.wantActive)
			}
		})
	}
}

func TestActivate(t *testing.T) {
	tests := []struct {
		name    string
		strict  bool
		state   State
		id      string
		wantErr error
	}{
		{name: "free order: any pending task", strict: false, state: NewState("c", t0), id: "plan"},
		{name: "strict order: first pending task", strict: true, state: NewState("c", t0), id: "teeth"},
		{name: "strict order: later task", strict: true, state: NewState("c", t0), id: "study", wantErr: ErrOutOfOrder},
		{name: "unknown task", strict: false, state: NewState("c", t0), id: "ghost", wantErr: ErrUnknownTask},
		{
			name:    "done task",
			strict:  false,
			state:   stateWith("", map[string]Progress{"teeth": {Done: true}}),
			id:      "teeth",
			wantErr: ErrAlreadyDone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tasks(tt.strict), tt.state)
			err := s.Activate(tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Activate(%q) = %v, want %v", tt.id, err, tt.wantErr)
			}
			if err == nil && s.State().Active != tt.id {
				t.Errorf("Active = %q, want %q", s.State().Active, tt.id)
			}
		})
	}
}

func TestTick(t *testing.T) {
	type tick struct {
		after time.Duration
		pause string
	}
	tests := []struct {
		name        string
		active      string
		ticks       []tick
		wantElapsed time.Duration
		wantRunning bool
	}{
		{
			name:        "first tick only sets the baseline",
			active:      "study",
			ticks:       []tick{{after: time.Second}},
			wantElapsed: 0,
			wantRunning: true,
		},
		{
			name:        "time accumulates between ticks",
			active:      "study",
			ticks:       []tick{{}, {after: 400 * time.Millisecond}, {after: 400 * time.Millisecond}},
			wantElapsed: 800 * time.Millisecond,
			wantRunning: true,
		},
		{
			name:        "pause stops the timer",
			active:      "study",
			ticks:       []tick{{}, {after: time.Second}, {after: time.Second, pause: "on"}, {after: time.Hour}},
			wantElapsed: time.Second,
			wantRunning: false,
		},
		{
			name:   "timer resumes after the pause without counting it",
			active: "study",
			ticks: []tick{
				{},
				{after: time.Second},
				{after: time.Second, pause: "on"},
				{after: 20 * time.Minute},
				{after: time.Second, pause: "off"},
				{after: time.Second},
			},
			wantElapsed: 3 * time.Second,
			wantRunning: true,
		},
		{
			name:        "long gap is capped",
			active:      "study",
			ticks:       []tick{{}, {after: 8 * time.Hour}},
			wantElapsed: maxTickGap,
			wantRunning: true,
		},
		{
			name:        "clock moved back adds nothing",
			active:      "study",
			ticks:       []tick{{}, {after: -time.Minute}},
			wantElapsed: 0,
			wantRunning: true,
		},
		{
			name:        "check task has no timer",
			active:      "teeth",
			ticks:       []tick{{}, {after: time.Second}},
			wantElapsed: 0,
			wantRunning: false,
		},
		{
			name:        "nothing active",
			active:      "",
			ticks:       []tick{{}, {after: time.Second}},
			wantElapsed: 0,
			wantRunning: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tasks(false), stateWith(tt.active, nil))
			now := t0
			var running bool
			for _, tk := range tt.ticks {
				if tk.pause != "" {
					s.SetPaused(tk.pause == "on")
				}
				now = now.Add(tk.after)
				running = s.Tick(now)
			}
			if got := s.State().Tasks["study"].Elapsed; got != tt.wantElapsed {
				t.Errorf("Elapsed = %v, want %v", got, tt.wantElapsed)
			}
			if running != tt.wantRunning {
				t.Errorf("running = %v, want %v", running, tt.wantRunning)
			}
		})
	}
}

func TestPause(t *testing.T) {
	t.Run("only a timer task can be paused", func(t *testing.T) {
		s := New(tasks(false), stateWith("teeth", nil))
		s.SetPaused(true)
		if s.Paused() {
			t.Error("check task was paused")
		}
		none := New(tasks(false), NewState("c", t0))
		none.SetPaused(true)
		if none.Paused() {
			t.Error("paused with no active task")
		}
	})

	t.Run("pause survives a restart", func(t *testing.T) {
		s := New(tasks(false), stateWith("study", nil))
		s.SetPaused(true)
		if restored := New(tasks(false), s.State()); !restored.Paused() {
			t.Error("pause was lost after restoring the session")
		}
	})

	t.Run("switching the task clears the pause", func(t *testing.T) {
		s := New(tasks(false), stateWith("study", nil))
		s.SetPaused(true)
		if err := s.Activate("study"); err != nil || !s.Paused() {
			t.Fatalf("re-activating the same task: err %v, paused %v; want pause kept", err, s.Paused())
		}
		if err := s.Activate("plan"); err != nil || s.Paused() {
			t.Errorf("switching task: err %v, paused %v; want pause cleared", err, s.Paused())
		}
	})

	t.Run("finishing the paused task clears the pause", func(t *testing.T) {
		st := stateWith("study", map[string]Progress{"study": {Elapsed: time.Hour}})
		s := New(tasks(false), st)
		s.SetPaused(true)
		if _, err := s.Complete("teeth", "", t0); err != nil || !s.Paused() {
			t.Fatalf("finishing another task: err %v, paused %v; want pause kept", err, s.Paused())
		}
		if _, err := s.Complete("study", "каналы", t0); err != nil || s.Paused() {
			t.Errorf("finishing the paused task: err %v, paused %v; want pause cleared", err, s.Paused())
		}
	})

	t.Run("pause of a removed task is dropped", func(t *testing.T) {
		st := stateWith("ghost", nil)
		st.Paused = true
		if New(tasks(false), st).Paused() {
			t.Error("pause kept for a task that no longer exists")
		}
	})
}

func TestComplete(t *testing.T) {
	studied := map[string]Progress{"teeth": {Done: true}, "study": {Elapsed: time.Hour}}

	tests := []struct {
		name       string
		strict     bool
		state      State
		id         string
		note       string
		wantErr    error
		wantActive string
		wantAll    bool
		wantEntry  bool
	}{
		{name: "check task", strict: true, state: NewState("c", t0), id: "teeth", wantActive: "study"},
		{name: "strict order: later task", strict: true, state: NewState("c", t0), id: "plan", wantErr: ErrOutOfOrder},
		{name: "free order: later task", strict: false, state: NewState("c", t0), id: "plan", wantActive: ""},
		{name: "unknown task", strict: false, state: NewState("c", t0), id: "ghost", wantErr: ErrUnknownTask},
		{
			name:    "already done",
			strict:  false,
			state:   stateWith("", map[string]Progress{"teeth": {Done: true}}),
			id:      "teeth",
			wantErr: ErrAlreadyDone,
		},
		{
			name:    "timer not finished",
			strict:  true,
			state:   stateWith("", map[string]Progress{"teeth": {Done: true}, "study": {Elapsed: 59 * time.Minute}}),
			id:      "study",
			note:    "горутины",
			wantErr: ErrTimerRunning,
		},
		{
			name:    "note required",
			strict:  true,
			state:   stateWith("", studied),
			id:      "study",
			note:    "   ",
			wantErr: ErrNoteRequired,
		},
		{
			name:       "timer task with note goes to journal",
			strict:     true,
			state:      stateWith("", studied),
			id:         "study",
			note:       "  разобрал каналы  ",
			wantActive: "plan",
			wantEntry:  true,
		},
		{
			name:   "last task finishes the cycle",
			strict: true,
			state: stateWith("", map[string]Progress{
				"teeth": {Done: true},
				"study": {Done: true, Elapsed: time.Hour},
			}),
			id:      "plan",
			wantAll: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tasks(tt.strict), tt.state)
			done := t0.Add(2 * time.Hour)

			res, err := s.Complete(tt.id, tt.note, done)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Complete(%q) = %v, want %v", tt.id, err, tt.wantErr)
			}
			if err != nil {
				if s.State().Tasks[tt.id].Done && !errors.Is(err, ErrAlreadyDone) {
					t.Error("task became done despite the error")
				}
				return
			}

			st := s.State()
			if p := st.Tasks[tt.id]; !p.Done || !p.DoneAt.Equal(done) {
				t.Errorf("progress = %+v, want done at %v", p, done)
			}
			if st.Active != tt.wantActive {
				t.Errorf("Active = %q, want %q", st.Active, tt.wantActive)
			}
			if res.AllDone != tt.wantAll || s.Done() != tt.wantAll {
				t.Errorf("AllDone = %v, Done() = %v, want %v", res.AllDone, s.Done(), tt.wantAll)
			}
			if st.FinishedAt.Equal(done) != tt.wantAll {
				t.Errorf("FinishedAt = %v, cycle finished = %v", st.FinishedAt, tt.wantAll)
			}
			if (res.Entry != nil) != tt.wantEntry {
				t.Fatalf("Entry = %+v, want entry: %v", res.Entry, tt.wantEntry)
			}
			if res.Entry != nil {
				want := domain.JournalEntry{
					Cycle: "2026-11-11", TaskID: "study", Title: "Учиться по roadmap",
					Note: "разобрал каналы", Minutes: 60, At: done,
				}
				if *res.Entry != want {
					t.Errorf("Entry = %+v, want %+v", *res.Entry, want)
				}
			}
		})
	}
}

func TestAllowedGroups(t *testing.T) {
	tests := []struct {
		name   string
		strict bool
		state  State
		want   []string
	}{
		{name: "nothing done, check task active", strict: true, state: NewState("c", t0), want: nil},
		{name: "free order, nothing chosen", strict: false, state: NewState("c", t0), want: nil},
		{name: "active task opens its programs", strict: false, state: stateWith("study", nil), want: []string{"ide", "web"}},
		{
			name:   "done task unlocks, active task adds",
			strict: true,
			state:  stateWith("", map[string]Progress{"teeth": {Done: true}}),
			want:   []string{"music", "ide", "web"},
		},
		{
			name:   "needs close after the task, unlocks stay",
			strict: true,
			state:  stateWith("", map[string]Progress{"teeth": {Done: true}, "study": {Done: true}}),
			want:   []string{"music", "web"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tasks(tt.strict), tt.state)
			if got := s.AllowedGroups(); !slices.Equal(got, tt.want) {
				t.Errorf("AllowedGroups() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		strict bool
		state  State
		want   []Status
	}{
		{
			name:   "strict order",
			strict: true,
			state:  stateWith("", map[string]Progress{"teeth": {Done: true}}),
			want:   []Status{StatusDone, StatusActive, StatusLocked},
		},
		{
			name:   "free order",
			strict: false,
			state:  stateWith("plan", nil),
			want:   []Status{StatusReady, StatusReady, StatusActive},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			views := New(tasks(tt.strict), tt.state).Snapshot()
			got := make([]Status, len(views))
			for i, v := range views {
				got[i] = v.Status
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("statuses = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("remaining time", func(t *testing.T) {
		st := stateWith("study", map[string]Progress{"study": {Elapsed: 45 * time.Minute}})
		views := New(tasks(false), st).Snapshot()
		if got := views[1].Remaining; got != 15*time.Minute {
			t.Errorf("Remaining = %v, want 15m", got)
		}
		if got := views[0].Remaining; got != 0 {
			t.Errorf("check task Remaining = %v, want 0", got)
		}
	})
}

func TestStateIsACopy(t *testing.T) {
	saved := stateWith("", nil)
	s := New(tasks(true), saved)
	if _, err := s.Complete("teeth", "", t0); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(saved.Tasks) != 0 {
		t.Error("New must not share the task map with the caller")
	}
	st := s.State()
	st.Tasks["teeth"] = Progress{}
	if !s.State().Tasks["teeth"].Done {
		t.Error("State must return a copy of the task map")
	}
}

func TestEmptyListIsDone(t *testing.T) {
	s := New(domain.TaskList{StrictOrder: true}, NewState("c", t0))
	if !s.Done() {
		t.Error("empty list must count as done")
	}
	if got := s.AllowedGroups(); len(got) != 0 {
		t.Errorf("AllowedGroups() = %q, want none", got)
	}
}
