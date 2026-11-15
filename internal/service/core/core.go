package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"dofirst/internal/domain"
	"dofirst/internal/service/guard"
	"dofirst/internal/service/rules"
	"dofirst/internal/service/schedule"
	"dofirst/internal/service/session"
	"dofirst/internal/service/stats"
	"dofirst/internal/storage"
)

var ErrNotLocked = errors.New("task list is not active")

const ReasonTest schedule.Reason = "test"

const (
	docSettings = "settings"
	docMarks    = "marks"
	docSession  = "session"
	docJournal  = "journal"
	docHistory  = "history"
	docDisables = "disables"

	testCycle     = "test"
	autosaveEvery = 30 * time.Second
	launchGrace   = 8 * time.Second
)

// документное хранилище
type Store interface {
	Load(name string, out any) error
	Save(name string, value any) error
}


type Screen interface {
	guard.Presenter
	Changed(v View)
}

type Deps struct {
	Store      Store
	Inspector  guard.Inspector
	Controller guard.Controller
	Screen     Screen
	Now        func() time.Time
	Log        *slog.Logger
}

type TaskView struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Kind         domain.TaskKind `json:"kind"`
	MinMinutes   int             `json:"minMinutes"`
	RequireNote  bool            `json:"requireNote"`
	Status       session.Status  `json:"status"`
	ElapsedSec   int             `json:"elapsedSec"`
	RemainingSec int             `json:"remainingSec"`
	Note         string          `json:"note"`
	// Needs и Unlocks — названия групп программ.
	Needs   []string `json:"needs"`
	Unlocks []string `json:"unlocks"`
	Launch  []string `json:"launch"`
}

type View struct {
	Phase  string `json:"phase"`
	Reason string `json:"reason"`
	Cycle  string `json:"cycle"`
	Test        bool   `json:"test"`
	StrictOrder bool   `json:"strictOrder"`
	Active      string `json:"active"`
	Running     bool   `json:"timerRunning"`
	Paused bool       `json:"paused"`
	Tasks  []TaskView `json:"tasks"`
	OpenGroups []string  `json:"openGroups"`
	Streak     int       `json:"streak"`
	Now        time.Time `json:"now"`
	Next       time.Time `json:"next"`
}

type Core struct {
	store  Store
	screen Screen
	now    func() time.Time
	log    *slog.Logger
	guard  *guard.Guard

	mu       sync.Mutex
	settings domain.Settings
	marks    schedule.Marks
	journal  []domain.JournalEntry
	history  []stats.Day
	disables []domain.DisableEvent
	fresh    bool

	status   schedule.Status
	sess     *session.Session
	test     bool
	running  bool
	revision int
	rulesKey string
	dirty    bool
	lastSave time.Time
}

func New(d Deps) (*Core, error) {
	c := &Core{store: d.Store, screen: d.Screen, now: d.Now, log: d.Log}

	found, err := c.load(docSettings, &c.settings)
	if err != nil {
		return nil, err
	}
	if !found {
		c.fresh = true
		c.settings = domain.DefaultSettings()
		if err := c.save(docSettings, c.settings); err != nil {
			return nil, err
		}
	}
	found, err = c.load(docMarks, &c.marks)
	if err != nil {
		return nil, err
	}
	if !found {
		c.marks = schedule.Marks{FirstRun: d.Now()}
		if err := c.save(docMarks, c.marks); err != nil {
			return nil, err
		}
	}
	for name, out := range map[string]any{docJournal: &c.journal, docHistory: &c.history, docDisables: &c.disables} {
		if _, err := c.load(name, out); err != nil {
			return nil, err
		}
	}

	c.guard = guard.New(d.Inspector, d.Controller, d.Screen, c.guardConfig(), d.Log)
	return c, nil
}

func (c *Core) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close()
			return
		case <-ticker.C:
			c.Tick(c.now())
		}
	}
}

func (c *Core) Tick(now time.Time) {
	c.mu.Lock()
	changed := c.advance(now)
	var v View
	if changed {
		v = c.view(now)
	}
	c.mu.Unlock()

	c.guard.Step(now)
	if changed {
		c.screen.Changed(v)
	}
}

func (c *Core) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saveSession()
}

func (c *Core) FirstLaunch() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fresh
}

func (c *Core) View() View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view(c.now())
}

// Settings возвращает действующие настройки.
func (c *Core) Settings() domain.Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings
}

func (c *Core) Journal() []domain.JournalEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]domain.JournalEntry(nil), c.journal...)
}

func (c *Core) History() ([]stats.Day, []domain.DisableEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]stats.Day(nil), c.history...), append([]domain.DisableEvent(nil), c.disables...)
}

func (c *Core) UpdateSettings(s domain.Settings) (View, error) {
	return c.act(func(now time.Time) error {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("settings: %w", err)
		}
		ids := make([]string, len(s.Groups))
		for i, g := range s.Groups {
			ids[i] = g.ID
		}
		if _, err := rules.Compile(s.Groups, ids); err != nil {
			return fmt.Errorf("settings: %w", err)
		}
		if err := c.save(docSettings, s); err != nil {
			return err
		}
		c.settings = s
		c.fresh = false
		c.revision++
		if c.sess != nil {
			state := c.sess.State()
			c.sess = session.New(c.tasksFor(state.Cycle, now), state)
		}
		c.guard.SetConfig(c.guardConfig())
		return nil
	})
}

// Activate делает задачу текущей.
func (c *Core) Activate(id string) (View, error) {
	return c.act(func(time.Time) error {
		if c.sess == nil {
			return ErrNotLocked
		}
		if err := c.sess.Activate(id); err != nil {
			return err
		}
		c.saveSession()
		return nil
	})
}

func (c *Core) Pause(paused bool) (View, error) {
	return c.act(func(time.Time) error {
		if c.sess == nil {
			return ErrNotLocked
		}
		c.sess.SetPaused(paused)
		c.saveSession()
		return nil
	})
}

func (c *Core) Complete(id, note string) (View, error) {
	return c.act(func(now time.Time) error {
		if c.sess == nil {
			return ErrNotLocked
		}
		res, err := c.sess.Complete(id, note, now)
		if err != nil {
			return err
		}
		if res.Entry != nil && !c.test {
			c.journal = append(c.journal, *res.Entry)
			err = c.save(docJournal, c.journal)
		}
		if res.AllDone {
			c.finishCycle(now)
		} else {
			c.saveSession()
		}
		return err
	})
}

func (c *Core) Disable(mode domain.DisableMode, reason string) (View, error) {
	return c.act(func(now time.Time) error {
		if c.test {
			c.test = false
			c.sess = nil
			return nil
		}
		st := c.status
		pending := st.Phase != schedule.PhaseFree ||
			st.Reason == schedule.ReasonArmed || st.Reason == schedule.ReasonSnoozed

		if !pending {
			return nil
		}

		switch mode {
		case domain.DisableQuit:
		case domain.DisableNight:
			c.marks.NightOffCycle = st.Cycle
		default:
			c.marks.DisabledCycle = st.Cycle
		}

		c.disables = append(c.disables, domain.DisableEvent{
			Cycle: st.Cycle, Mode: mode, Reason: strings.TrimSpace(reason), At: now,
		})
		return errors.Join(c.save(docMarks, c.marks), c.save(docDisables, c.disables))
	})
}

func (c *Core) SnoozeNight(d time.Duration) (View, error) {
	return c.act(func(now time.Time) error {
		c.marks.SnoozeUntil = now.Add(d)
		return c.save(docMarks, c.marks)
	})
}

func (c *Core) StartTest() (View, error) {
	return c.act(func(time.Time) error {
		if c.status.Phase == schedule.PhaseLocked {
			return nil
		}
		c.test = true
		return nil
	})
}

func (c *Core) Launched() {
	c.guard.Grace(c.now().Add(launchGrace))
}

func (c *Core) act(fn func(now time.Time) error) (View, error) {
	c.mu.Lock()
	now := c.now()
	err := fn(now)
	c.advance(now)
	v := c.view(now)
	c.mu.Unlock()

	c.screen.Changed(v)
	return v, err
}

func (c *Core) advance(now time.Time) bool {
	prev, prevRunning := c.status, c.running

	c.status = c.evaluate(now)
	if c.status.Phase == schedule.PhaseLocked {
		c.ensureSession(now)
		if c.sess.Done() {
			c.finishCycle(now)
		}
	}
	if c.status.Phase == schedule.PhaseLocked {
		c.running = c.sess.Tick(now)
		c.dirty = c.dirty || c.running
	} else {
		c.dropSession()
		c.running = false
	}

	c.applyRules()
	if c.dirty && now.Sub(c.lastSave) >= autosaveEvery {
		c.saveSession()
	}

	return c.status.Phase != prev.Phase || c.status.Cycle != prev.Cycle ||
		c.status.Reason != prev.Reason || c.running != prevRunning
}

func (c *Core) evaluate(now time.Time) schedule.Status {
	if c.test {
		return schedule.Status{Phase: schedule.PhaseLocked, Reason: ReasonTest, Cycle: testCycle}
	}
	return schedule.Evaluate(c.settings.Schedule, c.marks, now)
}

func (c *Core) ensureSession(now time.Time) {
	cycle := c.status.Cycle
	if c.sess != nil && c.sess.State().Cycle == cycle {
		return
	}
	c.dropSession()

	list := c.tasksFor(cycle, now)
	state := session.NewState(cycle, now)
	if !c.test {
		var saved session.State
		if found, err := c.load(docSession, &saved); err != nil {
			c.log.Error("load session", "err", err)
		} else if found && saved.Cycle == cycle {
			state = saved
		}
		if c.day(cycle) == nil && len(list.Tasks) > 0 {
			c.history = append(c.history, stats.Day{Cycle: cycle, LockedAt: now})
			c.logErr(c.save(docHistory, c.history))
		}
	}
	c.sess = session.New(list, state)
	c.saveSession()
}

func (c *Core) tasksFor(cycle string, now time.Time) domain.TaskList {
	day := now.Weekday()
	if date, err := time.Parse(schedule.CycleLayout, cycle); err == nil {
		day = date.Weekday()
	}
	return c.settings.Tasks.ForDay(day)
}

func (c *Core) finishCycle(now time.Time) {
	if c.test {
		c.test = false
		c.sess = nil
	} else {
		c.marks.DoneCycle = c.status.Cycle
		c.logErr(c.save(docMarks, c.marks))
		if d := c.day(c.status.Cycle); d != nil && !d.Done() {
			d.DoneAt = now
			c.logErr(c.save(docHistory, c.history))
		}
		c.dropSession()
	}
	c.status = c.evaluate(now)
	if c.status.Phase == schedule.PhaseLocked {
		c.ensureSession(now)
	}
}

func (c *Core) dropSession() {
	c.saveSession()
	c.sess = nil
}

func (c *Core) saveSession() {
	if c.sess == nil || c.test {
		return
	}
	c.logErr(c.save(docSession, c.sess.State()))
	c.dirty = false
	c.lastSave = c.now()
}

func (c *Core) applyRules() {
	var key string
	switch c.status.Phase {
	case schedule.PhaseLocked:
		key = fmt.Sprintf("L%d|%s", c.revision, strings.Join(c.sess.AllowedGroups(), "\x00"))
	case schedule.PhaseNight:
		key = fmt.Sprintf("N%d", c.revision)
	}
	if key == c.rulesKey {
		return
	}
	c.rulesKey = key
	if key == "" {
		c.guard.SetRules(nil)
		return
	}

	ids := c.settings.Schedule.NightGroups
	if c.status.Phase == schedule.PhaseLocked {
		ids = c.sess.AllowedGroups()
	}
	active, err := rules.Compile(c.settings.Groups, ids)
	if err != nil {
		c.log.Warn("some program groups are skipped", "err", err)
	}
	opts := rules.DefaultOptions()
	opts.Always = append(opts.Always, c.settings.AlwaysAllowed...)
	c.guard.SetRules(rules.NewSet(active, opts))
}

func (c *Core) view(now time.Time) View {
	v := View{
		Phase:       c.status.Phase.String(),
		Reason:      string(c.status.Reason),
		Cycle:       c.status.Cycle,
		Test:        c.test,
		StrictOrder: c.settings.Tasks.StrictOrder,
		Running:     c.running,
		Tasks:       []TaskView{},
		OpenGroups:  []string{},
		Streak:      stats.Streak(c.history, c.status.Cycle),
		Now:         now,
		Next:        c.status.Next,
	}
	names := make(map[string]string, len(c.settings.Groups))
	for _, g := range c.settings.Groups {
		names[g.ID] = g.Name
	}
	label := func(ids []string) []string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if name := names[id]; name != "" {
				out = append(out, name)
			}
		}
		return out
	}

	if c.status.Phase == schedule.PhaseNight {
		v.OpenGroups = label(c.settings.Schedule.NightGroups)
	}
	if c.sess == nil {
		return v
	}

	v.Active = c.sess.State().Active
	v.Paused = c.sess.Paused()
	v.OpenGroups = label(c.sess.AllowedGroups())
	for _, t := range c.sess.Snapshot() {
		v.Tasks = append(v.Tasks, TaskView{
			ID:           t.Task.ID,
			Title:        t.Task.Title,
			Description:  t.Task.Description,
			Kind:         t.Task.Kind,
			MinMinutes:   t.Task.MinMinutes,
			RequireNote:  t.Task.RequireNote,
			Status:       t.Status,
			ElapsedSec:   int(t.Elapsed / time.Second),
			RemainingSec: int((t.Remaining + time.Second - 1) / time.Second),
			Note:         t.Note,
			Needs:        label(t.Task.Needs),
			Unlocks:      label(t.Task.Unlocks),
			Launch:       append([]string{}, t.Task.Launch...),
		})
	}
	return v
}

func (c *Core) day(cycle string) *stats.Day {
	for i := len(c.history) - 1; i >= 0; i-- {
		if c.history[i].Cycle == cycle {
			return &c.history[i]
		}
	}
	return nil
}

func (c *Core) guardConfig() guard.Config {
	return guard.DefaultConfig(time.Duration(c.settings.TabCountdownSec) * time.Second)
}

func (c *Core) load(name string, out any) (bool, error) {
	err := c.store.Load(name, out)
	if errors.Is(err, storage.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load %s: %w", name, err)
	}
	return true, nil
}

func (c *Core) save(name string, value any) error {
	if err := c.store.Save(name, value); err != nil {
		return fmt.Errorf("save %s: %w", name, err)
	}
	return nil
}

func (c *Core) logErr(err error) {
	if err != nil {
		c.log.Error("storage", "err", err)
	}
}
