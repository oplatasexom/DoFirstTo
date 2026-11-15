package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"dofirst/internal/domain"
	"dofirst/internal/service/guard"
	"dofirst/internal/service/schedule"
	"dofirst/internal/service/session"
	"dofirst/internal/storage"
)

// хранилище в памяти
type memStore struct {
	docs    map[string][]byte
	loadErr error
	saveErr error
}

func newMemStore() *memStore { return &memStore{docs: map[string][]byte{}} }

func (m *memStore) Load(name string, out any) error {
	if m.loadErr != nil {
		return m.loadErr
	}
	raw, ok := m.docs[name]
	if !ok {
		return fmt.Errorf("load %q: %w", name, storage.ErrNotFound)
	}
	return json.Unmarshal(raw, out)
}

func (m *memStore) Save(name string, value any) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.docs[name] = raw
	return nil
}

type desk struct {
	t      time.Time
	win    guard.Window
	events []string
	views  []View
}

func (d *desk) now() time.Time                    { return d.t }
func (d *desk) Foreground() (guard.Window, error) { return d.win, nil }
func (d *desk) CloseTab(uintptr) (bool, error) {
	d.events = append(d.events, "close tab")
	return true, nil
}
func (d *desk) TabWarning(guard.Window, int) {}
func (d *desk) Changed(v View)               { d.views = append(d.views, v) }

func (d *desk) Allowed(guard.Window) {}

func (d *desk) Minimize(uintptr) error {
	d.events = append(d.events, "minimize")
	return nil
}

func (d *desk) Blocked(w guard.Window, _ guard.Reason) {
	d.events = append(d.events, "blocked "+w.Exe)
}

var (
	zone     = time.FixedZone("T", 3*3600)
	telegram = guard.Window{ID: 7, Exe: "telegram.exe", Title: "Telegram"}
	claude   = guard.Window{ID: 8, Exe: "chrome.exe", Title: "Claude - Google Chrome"}
	vscode   = guard.Window{ID: 9, Exe: "code.exe", Title: "main.go"}
)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, zone)
}

func newCore(t *testing.T, store *memStore, d *desk) *Core {
	t.Helper()
	c, err := New(Deps{
		Store: store, Inspector: d, Controller: d, Screen: d,
		Now: d.now, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func installed(t *testing.T, mutate func(*domain.Settings)) *memStore {
	t.Helper()
	store := newMemStore()
	d := &desk{t: at(9, 12, 0)}
	c := newCore(t, store, d)
	if mutate != nil {
		s := c.Settings()
		mutate(&s)
		if _, err := c.UpdateSettings(s); err != nil {
			t.Fatalf("UpdateSettings: %v", err)
		}
	}
	return store
}

func (d *desk) step(c *Core, to time.Time) {
	d.t = to
	c.Tick(to)
}

// must(t)(c.Complete(...))
func must(t *testing.T) func(View, error) View {
	return func(v View, err error) View {
		t.Helper()
		if err != nil {
			t.Fatalf("action failed: %v", err)
		}
		return v
	}
}

func TestFirstLaunchIsNeverLocked(t *testing.T) {
	store := newMemStore()
	d := &desk{t: at(11, 9, 0), win: telegram}
	c := newCore(t, store, d)

	if !c.FirstLaunch() {
		t.Error("FirstLaunch = false on an empty store")
	}
	d.step(c, at(11, 9, 0))
	d.step(c, at(11, 22, 59))

	if v := c.View(); v.Phase != "free" || v.Reason != string(schedule.ReasonNotArmed) {
		t.Errorf("phase/reason = %s/%s, want free/not-armed", v.Phase, v.Reason)
	}
	if len(d.events) != 0 {
		t.Errorf("guard acted on the first day: %q", d.events)
	}
	if _, ok := store.docs[docSettings]; !ok {
		t.Error("default settings were not saved")
	}

	d.step(c, at(12, 7, 0))
	if v := c.View(); v.Phase != "locked" {
		t.Errorf("next morning phase = %s, want locked", v.Phase)
	}
	if newCore(t, store, d).FirstLaunch() {
		t.Error("FirstLaunch = true on the second start")
	}
}

func TestMorningRoutine(t *testing.T) {
	store := installed(t, nil)
	d := &desk{t: at(11, 7, 5), win: telegram}
	c := newCore(t, store, d)

	d.step(c, at(11, 7, 5))
	if len(d.views) != 1 || d.views[0].Phase != "locked" {
		t.Fatalf("views = %+v, want one locked view", d.views)
	}
	v := d.views[0]
	if len(v.Tasks) != 2 || v.Active != "teeth" || v.Tasks[1].Status != session.StatusLocked {
		t.Fatalf("view = %+v, want two tasks with teeth active", v)
	}
	if want := []string{"minimize", "blocked telegram.exe"}; !slices.Equal(d.events, want) {
		t.Fatalf("events = %q, want %q", d.events, want)
	}

	d.events = nil
	d.win = claude
	d.step(c, at(11, 7, 6))
	if want := []string{"minimize", "blocked chrome.exe"}; !slices.Equal(d.events, want) {
		t.Fatalf("events = %q, want %q", d.events, want)
	}

	v = must(t)(c.Complete("teeth", ""))
	if v.Active != "study" || !slices.Equal(v.OpenGroups, []string{"Учёба в браузере", "Редактор кода"}) {
		t.Fatalf("after teeth: active = %q, open = %q", v.Active, v.OpenGroups)
	}
	d.events = nil
	for _, w := range []guard.Window{claude, vscode} {
		d.win = w
		d.step(c, d.t.Add(time.Second))
	}
	if len(d.events) != 0 {
		t.Fatalf("allowed programs were blocked: %q", d.events)
	}

	if _, err := c.Complete("study", "рано"); !errors.Is(err, session.ErrTimerRunning) {
		t.Fatalf("Complete before timer = %v, want %v", err, session.ErrTimerRunning)
	}

	for range 3600 {
		d.step(c, d.t.Add(time.Second))
	}
	if _, err := c.Complete("study", ""); !errors.Is(err, session.ErrNoteRequired) {
		t.Fatalf("Complete without note = %v, want %v", err, session.ErrNoteRequired)
	}
	v = must(t)(c.Complete("study", "разобрал каналы"))

	if v.Phase != "free" || v.Reason != string(schedule.ReasonDone) || v.Streak != 1 {
		t.Fatalf("after all tasks: %s/%s streak %d, want free/done streak 1", v.Phase, v.Reason, v.Streak)
	}
	d.events = nil
	d.win = telegram
	d.step(c, d.t.Add(time.Second))
	if len(d.events) != 0 {
		t.Errorf("guard is still active after all tasks: %q", d.events)
	}
	journal := c.Journal()
	if len(journal) != 1 || journal[0].Note != "разобрал каналы" || journal[0].Minutes != 60 {
		t.Errorf("journal = %+v, want one study entry of 60 minutes", journal)
	}
	days, _ := c.History()
	if len(days) != 1 || !days[0].Done() || !days[0].LockedAt.Equal(at(11, 7, 5)) {
		t.Errorf("history = %+v, want one finished day locked at 07:05", days)
	}
	d.step(c, at(12, 7, 0))
	if v := c.View(); v.Phase != "locked" || v.Active != "teeth" || v.Streak != 1 {
		t.Errorf("next morning: %s active %q streak %d, want locked teeth 1", v.Phase, v.Active, v.Streak)
	}
}

func TestManualPause(t *testing.T) {
	store := installed(t, nil)
	d := &desk{t: at(11, 7, 5)}
	c := newCore(t, store, d)
	d.step(c, at(11, 7, 5))

	if v := must(t)(c.Pause(true)); v.Paused {
		t.Fatal("check task was paused")
	}
	must(t)(c.Complete("teeth", ""))
	for range 10 {
		d.step(c, d.t.Add(time.Second))
	}
	if v := c.View(); !v.Running || v.Paused {
		t.Fatalf("running = %v, paused = %v; want a running timer", v.Running, v.Paused)
	}
	if v := must(t)(c.Pause(true)); !v.Paused || v.Running {
		t.Fatalf("after pause: paused = %v, running = %v", v.Paused, v.Running)
	}
	for range 5 {
		d.step(c, d.t.Add(10*time.Minute))
	}
	if v := c.View(); !v.Paused || v.Tasks[1].ElapsedSec != 10 {
		t.Fatalf("during pause: paused = %v, elapsed = %ds; want paused at 10s", v.Paused, v.Tasks[1].ElapsedSec)
	}

	c.Close()
	c = newCore(t, store, d)
	d.step(c, d.t.Add(time.Second))
	if v := c.View(); !v.Paused || v.Tasks[1].ElapsedSec != 10 {
		t.Fatalf("after restart: paused = %v, elapsed = %ds; want paused at 10s", v.Paused, v.Tasks[1].ElapsedSec)
	}

	must(t)(c.Pause(false))
	for range 5 {
		d.step(c, d.t.Add(time.Second))
	}
	if v := c.View(); v.Paused || !v.Running || v.Tasks[1].ElapsedSec != 15 {
		t.Errorf("after resume: paused = %v, running = %v, elapsed = %ds; want running at 15s", v.Paused, v.Running, v.Tasks[1].ElapsedSec)
	}

	free := newCore(t, installed(t, nil), &desk{t: at(9, 15, 0)})
	if _, err := free.Pause(true); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Pause outside the lock = %v, want %v", err, ErrNotLocked)
	}
}

func TestProgressSurvivesRestart(t *testing.T) {
	store := installed(t, nil)
	d := &desk{t: at(11, 7, 5)}
	c := newCore(t, store, d)
	d.step(c, at(11, 7, 5))
	must(t)(c.Complete("teeth", ""))
	for range 120 {
		d.step(c, d.t.Add(time.Second))
	}
	c.Close()

	d2 := &desk{t: at(11, 9, 0)}
	c2 := newCore(t, store, d2)
	d2.step(c2, at(11, 9, 0))
	v := c2.View()
	if v.Active != "study" || v.Tasks[0].Status != session.StatusDone || v.Tasks[1].ElapsedSec != 120 {
		t.Errorf("restored view = %+v, want teeth done and 120s of study", v)
	}
	if days, _ := c2.History(); len(days) != 1 {
		t.Errorf("history has %d days, want 1: restart must not add a day", len(days))
	}
}

func TestDisable(t *testing.T) {
	tests := []struct {
		name        string
		settings    func(*domain.Settings)
		now         time.Time
		mode        domain.DisableMode
		wantPhase   string
		wantReason  schedule.Reason
		wantEvents  int
		wantMorning string
	}{
		{
			name: "until tomorrow from the task screen",
			now:  at(11, 7, 30), mode: domain.DisableUntilTomorrow,
			wantPhase: "free", wantReason: schedule.ReasonDisabled, wantEvents: 1, wantMorning: "free",
		},
		{
			name: "quit leaves the lock pending",
			now:  at(11, 7, 30), mode: domain.DisableQuit,
			wantPhase: "locked", wantReason: schedule.ReasonTasks, wantEvents: 1, wantMorning: "locked",
		},
		{
			name:     "skip the night screen only",
			settings: func(s *domain.Settings) { s.Schedule.NightScreen = true },
			now:      at(10, 23, 30), mode: domain.DisableNight,
			wantPhase: "free", wantReason: schedule.ReasonArmed, wantEvents: 1, wantMorning: "locked",
		},
		{
			name: "until tomorrow at night cancels the morning too",
			now:  at(10, 23, 30), mode: domain.DisableUntilTomorrow,
			wantPhase: "free", wantReason: schedule.ReasonDisabled, wantEvents: 1, wantMorning: "free",
		},
		{
			name:     "nothing to disable on a day off",
			settings: func(s *domain.Settings) { s.Schedule.Days = []time.Weekday{time.Monday} },
			now:      at(11, 9, 0), mode: domain.DisableUntilTomorrow,
			wantPhase: "free", wantReason: schedule.ReasonDayOff, wantEvents: 0, wantMorning: "free",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := installed(t, tt.settings)
			d := &desk{t: tt.now}
			c := newCore(t, store, d)
			d.step(c, tt.now)

			v := must(t)(c.Disable(tt.mode, "  встреча  "))
			if v.Phase != tt.wantPhase || v.Reason != string(tt.wantReason) {
				t.Errorf("after disable: %s/%s, want %s/%s", v.Phase, v.Reason, tt.wantPhase, tt.wantReason)
			}
			_, events := c.History()
			if len(events) != tt.wantEvents {
				t.Fatalf("disable events = %+v, want %d", events, tt.wantEvents)
			}
			if len(events) == 1 && (events[0].Reason != "встреча" || events[0].Mode != tt.mode) {
				t.Errorf("event = %+v, want trimmed reason and mode %s", events[0], tt.mode)
			}

			d.step(c, at(11, 7, 45))
			if got := c.View().Phase; got != tt.wantMorning {
				t.Errorf("morning phase = %s, want %s", got, tt.wantMorning)
			}
		})
	}
}

func TestNightScreen(t *testing.T) {
	store := installed(t, func(s *domain.Settings) { s.Schedule.NightScreen = true })
	d := &desk{t: at(10, 23, 30), win: claude}
	c := newCore(t, store, d)

	d.step(c, at(10, 23, 30))
	if v := c.View(); v.Phase != "night" || len(v.Tasks) != 0 {
		t.Fatalf("view = %+v, want night without tasks", v)
	}
	if want := []string{"minimize", "blocked chrome.exe"}; !slices.Equal(d.events, want) {
		t.Fatalf("events = %q, want %q", d.events, want)
	}

	d.events = nil
	if v := must(t)(c.SnoozeNight(15 * time.Minute)); v.Phase != "free" || v.Reason != string(schedule.ReasonSnoozed) {
		t.Fatalf("after snooze: %s/%s, want free/snoozed", v.Phase, v.Reason)
	}
	d.step(c, at(10, 23, 40))
	if len(d.events) != 0 {
		t.Fatalf("guard acted during snooze: %q", d.events)
	}
	d.step(c, at(10, 23, 45))
	if c.View().Phase != "night" || len(d.events) == 0 {
		t.Errorf("night screen did not return after snooze: phase %s, events %q", c.View().Phase, d.events)
	}
}

func TestNightGroups(t *testing.T) {
	store := installed(t, func(s *domain.Settings) {
		s.Schedule.NightScreen = true
		s.Schedule.NightGroups = []string{"ide"}
	})
	d := &desk{t: at(10, 23, 30), win: vscode}
	c := newCore(t, store, d)

	d.step(c, at(10, 23, 30))
	if v := c.View(); v.Phase != "night" || !slices.Equal(v.OpenGroups, []string{"Редактор кода"}) {
		t.Fatalf("view: phase %s, open %q; want night with the editor group", v.Phase, v.OpenGroups)
	}
	if len(d.events) != 0 {
		t.Fatalf("program from a night group was blocked: %q", d.events)
	}

	d.win = claude
	d.step(c, at(10, 23, 31))
	if want := []string{"minimize", "blocked chrome.exe"}; !slices.Equal(d.events, want) {
		t.Errorf("events = %q, want %q", d.events, want)
	}
}

func TestTestMode(t *testing.T) {
	store := installed(t, nil)
	d := &desk{t: at(9, 15, 0), win: telegram}
	c := newCore(t, store, d)
	d.step(c, at(9, 15, 0))
	if c.View().Phase != "free" {
		t.Fatalf("phase = %s, want free before the test", c.View().Phase)
	}

	v := must(t)(c.StartTest())
	if v.Phase != "locked" || !v.Test || v.Active != "teeth" {
		t.Fatalf("test view = %+v, want locked test with teeth active", v)
	}
	d.step(c, at(9, 15, 1))
	if want := []string{"minimize", "blocked telegram.exe"}; !slices.Equal(d.events, want) {
		t.Fatalf("events = %q, want %q", d.events, want)
	}

	v = must(t)(c.Disable(domain.DisableUntilTomorrow, "проверил"))
	if v.Phase != "free" || v.Test {
		t.Fatalf("after disable: %+v, want free", v)
	}
	days, events := c.History()
	if len(days) != 0 || len(events) != 0 || len(c.Journal()) != 0 {
		t.Errorf("test mode left traces: days %v, events %v", days, events)
	}
	if _, ok := store.docs[docSession]; ok {
		t.Error("test session was saved")
	}

	must(t)(c.StartTest())
	must(t)(c.Complete("teeth", ""))
	for range 3600 {
		d.step(c, d.t.Add(time.Second))
	}
	v = must(t)(c.Complete("study", "проба"))
	if v.Phase != "free" || v.Test || v.Streak != 0 || len(c.Journal()) != 0 {
		t.Errorf("after finished test: %+v, want free with no streak and no journal", v)
	}
}

func TestUpdateSettings(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*domain.Settings)
		wantErr string
	}{
		{name: "valid change", mutate: func(s *domain.Settings) { s.Tasks.Tasks[0].Title = "Зарядка" }},
		{name: "invalid structure", mutate: func(s *domain.Settings) { s.Tasks.Tasks[0].Title = "" }, wantErr: "empty title"},
		{name: "invalid rule", mutate: func(s *domain.Settings) { s.Groups[0].Rule = "| Claude" }, wantErr: "no programs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := installed(t, nil)
			d := &desk{t: at(11, 7, 5)}
			c := newCore(t, store, d)
			d.step(c, at(11, 7, 5))

			s := c.Settings()
			s.Tasks.Tasks = slices.Clone(s.Tasks.Tasks)
			s.Groups = slices.Clone(s.Groups)
			tt.mutate(&s)
			v, err := c.UpdateSettings(s)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("UpdateSettings = %v, want error containing %q", err, tt.wantErr)
				}
				if got := c.Settings().Tasks.Tasks[0].Title; got != "Почистить зубы" {
					t.Errorf("rejected settings were applied: title = %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateSettings: %v", err)
			}
			if v.Tasks[0].Title != "Зарядка" {
				t.Errorf("view title = %q, want the new one", v.Tasks[0].Title)
			}
			if got := newCore(t, store, d).Settings().Tasks.Tasks[0].Title; got != "Зарядка" {
				t.Errorf("saved title = %q, want the new one", got)
			}
		})
	}
}

func TestRemovingTasksKeepsProgressOfTheRest(t *testing.T) {
	store := installed(t, nil)
	d := &desk{t: at(11, 7, 5)}
	c := newCore(t, store, d)
	d.step(c, at(11, 7, 5))
	must(t)(c.Complete("teeth", ""))

	s := c.Settings()
	s.Tasks.Tasks = []domain.Task{s.Tasks.Tasks[0], {ID: "plan", Title: "План", Kind: domain.KindCheck}}
	v := must(t)(c.UpdateSettings(s))
	if len(v.Tasks) != 2 || v.Tasks[0].Status != session.StatusDone || v.Active != "plan" {
		t.Errorf("view = %+v, want teeth done and plan active", v)
	}
}

func TestTaskWeekdays(t *testing.T) {
	t.Run("task appears only on its days", func(t *testing.T) {
		store := installed(t, func(s *domain.Settings) {
			s.Tasks.Tasks[1].Days = []time.Weekday{time.Thursday}
		})
		d := &desk{t: at(11, 7, 5)}
		c := newCore(t, store, d)
		d.step(c, at(11, 7, 5))

		v := c.View()
		if len(v.Tasks) != 1 || v.Tasks[0].ID != "teeth" {
			t.Fatalf("wednesday tasks = %+v, want only teeth", v.Tasks)
		}
		if v = must(t)(c.Complete("teeth", "")); v.Phase != "free" || v.Reason != string(schedule.ReasonDone) {
			t.Errorf("after the only task phase/reason = %s/%s, want free/done", v.Phase, v.Reason)
		}

		d.step(c, at(12, 7, 5))
		if v = c.View(); len(v.Tasks) != 2 || v.Tasks[1].ID != "study" {
			t.Errorf("thursday tasks = %+v, want teeth and study", v.Tasks)
		}
	})

	t.Run("day without tasks does not lock and leaves no trace", func(t *testing.T) {
		store := installed(t, func(s *domain.Settings) {
			for i := range s.Tasks.Tasks {
				s.Tasks.Tasks[i].Days = []time.Weekday{time.Thursday}
			}
		})
		d := &desk{t: at(11, 7, 5), win: telegram}
		c := newCore(t, store, d)
		d.step(c, at(11, 7, 5))

		if v := c.View(); v.Phase != "free" || len(v.Tasks) != 0 {
			t.Errorf("wednesday phase = %s with %d tasks, want free and none", v.Phase, len(v.Tasks))
		}
		if len(d.events) != 0 {
			t.Errorf("guard acted on a day without tasks: %q", d.events)
		}
		if days, _ := c.History(); len(days) != 0 {
			t.Errorf("history = %+v, want no record for a day without tasks", days)
		}

		d.step(c, at(12, 7, 5))
		if v := c.View(); v.Phase != "locked" || len(v.Tasks) != 2 {
			t.Errorf("thursday phase = %s with %d tasks, want locked and two", v.Phase, len(v.Tasks))
		}
	})

	t.Run("changing days during the morning keeps progress", func(t *testing.T) {
		store := installed(t, nil)
		d := &desk{t: at(11, 7, 5)}
		c := newCore(t, store, d)
		d.step(c, at(11, 7, 5))
		must(t)(c.Complete("teeth", ""))

		s := c.Settings()
		s.Tasks.Tasks = append(s.Tasks.Tasks,
			domain.Task{ID: "gym", Title: "Зал", Kind: domain.KindCheck, Days: []time.Weekday{time.Friday}},
			domain.Task{ID: "plan", Title: "План", Kind: domain.KindCheck, Days: []time.Weekday{time.Wednesday}})
		v := must(t)(c.UpdateSettings(s))
		if len(v.Tasks) != 3 || v.Tasks[0].Status != session.StatusDone || v.Tasks[2].ID != "plan" {
			t.Errorf("tasks = %+v, want teeth done, study and plan", v.Tasks)
		}
	})
}

func TestActionsOutsideLock(t *testing.T) {
	store := installed(t, nil)
	d := &desk{t: at(9, 15, 0)}
	c := newCore(t, store, d)
	d.step(c, at(9, 15, 0))

	if _, err := c.Complete("teeth", ""); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Complete = %v, want %v", err, ErrNotLocked)
	}
	if _, err := c.Activate("teeth"); !errors.Is(err, ErrNotLocked) {
		t.Errorf("Activate = %v, want %v", err, ErrNotLocked)
	}
}

func TestFreeOrderActivate(t *testing.T) {
	store := installed(t, func(s *domain.Settings) { s.Tasks.StrictOrder = false })
	d := &desk{t: at(11, 7, 5), win: vscode}
	c := newCore(t, store, d)
	d.step(c, at(11, 7, 5))
	if want := []string{"minimize", "blocked code.exe"}; !slices.Equal(d.events, want) {
		t.Fatalf("events = %q, want %q", d.events, want)
	}

	v := must(t)(c.Activate("study"))
	if v.Active != "study" || len(v.OpenGroups) != 2 {
		t.Fatalf("view = %+v, want study active with two open groups", v)
	}
	d.events = nil
	d.step(c, at(11, 7, 6))
	if len(d.events) != 0 {
		t.Errorf("editor is blocked although its task is active: %q", d.events)
	}
}

func TestLaunchedGivesGrace(t *testing.T) {
	store := installed(t, func(s *domain.Settings) { s.TabCountdownSec = 0 })
	d := &desk{t: at(11, 7, 5)}
	c := newCore(t, store, d)
	d.step(c, at(11, 7, 5))
	must(t)(c.Complete("teeth", ""))

	d.win = guard.Window{ID: 8, Exe: "chrome.exe", Title: "Загрузка…"}
	c.Launched()
	d.step(c, d.t.Add(2*time.Second))
	if len(d.events) != 0 {
		t.Fatalf("tab closed during launch grace: %q", d.events)
	}
	d.step(c, d.t.Add(launchGrace))
	if want := []string{"close tab"}; !slices.Equal(d.events, want) {
		t.Errorf("events = %q, want %q", d.events, want)
	}
}

func TestStorageErrors(t *testing.T) {
	boom := errors.New("disk failure")

	t.Run("unreadable store stops startup", func(t *testing.T) {
		store := newMemStore()
		store.loadErr = boom
		d := &desk{t: at(11, 9, 0)}
		_, err := New(Deps{
			Store: store, Inspector: d, Controller: d, Screen: d,
			Now: d.now, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if !errors.Is(err, boom) {
			t.Errorf("New = %v, want %v", err, boom)
		}
	})

	t.Run("failed save is reported and settings stay", func(t *testing.T) {
		store := installed(t, nil)
		d := &desk{t: at(9, 15, 0)}
		c := newCore(t, store, d)
		store.saveErr = boom

		s := c.Settings()
		s.TabCountdownSec = 10
		if _, err := c.UpdateSettings(s); !errors.Is(err, boom) {
			t.Errorf("UpdateSettings = %v, want %v", err, boom)
		}
		if c.Settings().TabCountdownSec == 10 {
			t.Error("settings changed although they were not saved")
		}
	})
}
