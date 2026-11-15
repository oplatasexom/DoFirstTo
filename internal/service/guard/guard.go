package guard

import (
	"log/slog"
	"sync"
	"time"

	"dofirst/internal/service/rules"
)

type Window struct {
	ID    uintptr
	Exe   string
	Title string
	Own   bool
}

// сообщает, какое окно сейчас активно
type Inspector interface {
	Foreground() (Window, error)
}

type Controller interface {
	Minimize(id uintptr) error
	CloseTab(id uintptr) (bool, error)
}

// почему окно заблокировано
type Reason int

const (
	ReasonApp Reason = iota
	ReasonTabs
)

type Presenter interface {
	Blocked(w Window, reason Reason)
	TabWarning(w Window, seconds int)
	Allowed(w Window)
}

type Config struct {
	Countdown    time.Duration
	NeutralGrace time.Duration
	MaxStrikes   int
	StrikeWindow time.Duration
}

func DefaultConfig(countdown time.Duration) Config {
	return Config{
		Countdown:    countdown,
		NeutralGrace: 45 * time.Second,
		MaxStrikes:   3,
		StrikeWindow: max(time.Minute, 4*countdown),
	}
}

type Guard struct {
	insp Inspector
	ctl  Controller
	ui   Presenter
	log  *slog.Logger

	mu         sync.Mutex
	cfg        Config
	set        *rules.Set
	graceUntil time.Time

	warnID       uintptr
	deadline     time.Time
	shownSec     int
	neutralID    uintptr
	neutralSince time.Time
	strikes      []time.Time
	passedID     uintptr
}

func New(insp Inspector, ctl Controller, ui Presenter, cfg Config, log *slog.Logger) *Guard {
	return &Guard{insp: insp, ctl: ctl, ui: ui, cfg: cfg, log: log}
}

func (g *Guard) SetRules(set *rules.Set) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.set = set
	if set == nil {
		g.clearWarning()
		g.neutralID = 0
		g.passedID = 0
		g.strikes = nil
	}
}

func (g *Guard) SetConfig(cfg Config) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cfg = cfg
}

func (g *Guard) Grace(until time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.graceUntil = until
	g.strikes = nil
}

func (g *Guard) Step(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.set == nil {
		return
	}
	w, err := g.insp.Foreground()
	if err != nil {
		g.log.Debug("foreground window unavailable", "err", err)
		g.clearWarning()
		return
	}
	if w.ID == 0 {
		g.clearWarning()
		return
	}
	if w.Own {
		g.clearWarning()
		g.neutralID, g.passedID = 0, 0
		return
	}

	verdict := g.set.Check(rules.Window{Exe: w.Exe, Title: w.Title})
	if verdict == rules.Neutral {
		if g.neutralID != w.ID {
			g.neutralID, g.neutralSince = w.ID, now
		}
		if now.Sub(g.neutralSince) < g.cfg.NeutralGrace {
			g.clearWarning()
			g.pass(w)
			return
		}
		verdict = rules.BlockTitle
	} else {
		g.neutralID = 0
	}

	switch verdict {
	case rules.BlockApp:
		g.clearWarning()
		g.passedID = 0
		g.block(w, ReasonApp)
	case rules.BlockTitle:
		g.passedID = 0
		g.forbiddenTab(w, now)
	default:
		g.clearWarning()
		g.pass(w)
	}
}

func (g *Guard) pass(w Window) {
	if g.passedID == w.ID {
		return
	}
	g.passedID = w.ID
	g.ui.Allowed(w)
}

func (g *Guard) forbiddenTab(w Window, now time.Time) {
	if now.Before(g.graceUntil) {
		g.clearWarning()
		return
	}
	if g.warnID != w.ID {
		g.warnID, g.deadline, g.shownSec = w.ID, now.Add(g.cfg.Countdown), -1
	}
	if left := g.deadline.Sub(now); left > 0 {
		if sec := int((left + time.Second - 1) / time.Second); sec != g.shownSec {
			g.shownSec = sec
			g.ui.TabWarning(w, sec)
		}
		return
	}

	sent, err := g.ctl.CloseTab(w.ID)
	if err != nil {
		g.log.Warn("close tab failed", "exe", w.Exe, "err", err)
		g.clearWarning()
		g.block(w, ReasonTabs)
		return
	}
	if !sent {
		return
	}
	g.clearWarning()

	g.strikes = append(g.strikes, now)
	for len(g.strikes) > 0 && now.Sub(g.strikes[0]) > g.cfg.StrikeWindow {
		g.strikes = g.strikes[1:]
	}
	if len(g.strikes) >= g.cfg.MaxStrikes {
		g.strikes = nil
		g.block(w, ReasonTabs)
	}
}

func (g *Guard) block(w Window, reason Reason) {
	if err := g.ctl.Minimize(w.ID); err != nil {
		g.log.Warn("minimize failed", "exe", w.Exe, "err", err)
	}
	g.ui.Blocked(w, reason)
}

func (g *Guard) clearWarning() {
	if g.warnID == 0 {
		return
	}
	shown := g.shownSec > 0
	g.warnID, g.shownSec = 0, 0
	if shown {
		g.ui.TabWarning(Window{}, 0)
	}
}
