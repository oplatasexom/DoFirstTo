package guard

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"dofirst/internal/service/rules"
)

type world struct {
	win      Window
	winErr   error
	closeOK  bool
	closeErr error
	events   []string
	passed   []string
}

func (w *world) Allowed(win Window) { w.passed = append(w.passed, win.Exe) }

func (w *world) Foreground() (Window, error) { return w.win, w.winErr }

func (w *world) Minimize(id uintptr) error {
	w.events = append(w.events, fmt.Sprintf("minimize %d", id))
	return nil
}

func (w *world) CloseTab(id uintptr) (bool, error) {
	if w.closeErr != nil {
		return false, w.closeErr
	}
	if !w.closeOK {
		w.events = append(w.events, "close skipped")
		return false, nil
	}
	w.events = append(w.events, fmt.Sprintf("close tab %d", id))
	return true, nil
}

func (w *world) Blocked(win Window, reason Reason) {
	name := map[Reason]string{ReasonApp: "app", ReasonTabs: "tabs"}[reason]
	w.events = append(w.events, fmt.Sprintf("blocked %s (%s)", win.Exe, name))
}

func (w *world) TabWarning(_ Window, seconds int) {
	w.events = append(w.events, fmt.Sprintf("warn %d", seconds))
}

var (
	start = time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)

	chromeOK   = Window{ID: 1, Exe: "chrome.exe", Title: "Claude - Google Chrome"}
	chromeBad  = Window{ID: 1, Exe: "chrome.exe", Title: "YouTube - Google Chrome"}
	chromeBad2 = Window{ID: 1, Exe: "chrome.exe", Title: "Twitch - Google Chrome"}
	chromeNew  = Window{ID: 1, Exe: "chrome.exe", Title: "Новая вкладка - Google Chrome"}
	telegram   = Window{ID: 2, Exe: "telegram.exe", Title: "Telegram"}
	overlay    = Window{ID: 3, Exe: "dofirst.exe", Title: "DoFirstTo", Own: true}
	unknown    = Window{ID: 4}
)

func testConfig() Config {
	return Config{Countdown: 3 * time.Second, NeutralGrace: 10 * time.Second, MaxStrikes: 3, StrikeWindow: time.Minute}
}

func newGuard(t *testing.T, w *world, cfg Config) *Guard {
	t.Helper()
	rule, err := rules.Parse("chrome.exe | Claude, roadmap.sh")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	g := New(w, w, w, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	g.SetRules(rules.NewSet([]rules.Rule{rule}, rules.DefaultOptions()))
	return g
}

type frame struct {
	after time.Duration
	win   Window
}

func play(g *Guard, w *world, frames []frame) {
	now := start
	for _, f := range frames {
		now = now.Add(f.after)
		w.win = f.win
		g.Step(now)
	}
}

func TestStep(t *testing.T) {
	const sec = time.Second

	tests := []struct {
		name    string
		cfg     func(*Config)
		closeOK bool
		frames  []frame
		want    []string
	}{
		{
			name:   "allowed windows are left alone",
			frames: []frame{{0, chromeOK}, {sec, overlay}, {sec, unknown}, {sec, Window{}}},
			want:   nil,
		},
		{
			name:   "forbidden program is minimized",
			frames: []frame{{0, telegram}},
			want:   []string{"minimize 2", "blocked telegram.exe (app)"},
		},
		{
			name:    "forbidden tab: countdown, then close",
			closeOK: true,
			frames:  []frame{{0, chromeBad}, {sec / 2, chromeBad}, {sec / 2, chromeBad}, {sec, chromeBad}, {sec, chromeBad}},
			want:    []string{"warn 3", "warn 2", "warn 1", "close tab 1", "warn 0"},
		},
		{
			name:   "switching to an allowed tab cancels the countdown",
			frames: []frame{{0, chromeBad}, {sec, chromeBad}, {sec, chromeOK}, {5 * sec, chromeOK}},
			want:   []string{"warn 3", "warn 2", "warn 0"},
		},
		{
			name:    "hopping between forbidden tabs does not restart the countdown",
			closeOK: true,
			frames:  []frame{{0, chromeBad}, {sec, chromeBad2}, {sec, chromeBad}, {sec, chromeBad2}},
			want:    []string{"warn 3", "warn 2", "warn 1", "close tab 1", "warn 0"},
		},
		{
			name:    "leaving the browser cancels the countdown",
			closeOK: true,
			frames:  []frame{{0, chromeBad}, {sec, overlay}, {5 * sec, chromeBad}},
			want:    []string{"warn 3", "warn 0", "warn 3"},
		},
		{
			name:    "close is retried while it is unsafe",
			closeOK: false,
			frames:  []frame{{0, chromeBad}, {3 * sec, chromeBad}, {sec, chromeBad}},
			want:    []string{"warn 3", "close skipped", "close skipped"},
		},
		{
			name:    "zero countdown closes at once",
			cfg:     func(c *Config) { c.Countdown = 0 },
			closeOK: true,
			frames:  []frame{{0, chromeBad}},
			want:    []string{"close tab 1"},
		},
		{
			name:    "three closed tabs in a row minimize the browser",
			cfg:     func(c *Config) { c.Countdown = 0 },
			closeOK: true,
			frames:  []frame{{0, chromeBad}, {sec, chromeBad2}, {sec, chromeBad}},
			want:    []string{"close tab 1", "close tab 1", "close tab 1", "minimize 1", "blocked chrome.exe (tabs)"},
		},
		{
			name:    "old strikes expire",
			cfg:     func(c *Config) { c.Countdown = 0 },
			closeOK: true,
			frames:  []frame{{0, chromeBad}, {sec, chromeBad}, {2 * time.Minute, chromeBad}},
			want:    []string{"close tab 1", "close tab 1", "close tab 1"},
		},
		{
			name:   "new tab gets a grace period",
			frames: []frame{{0, chromeNew}, {9 * sec, chromeNew}},
			want:   nil,
		},
		{
			name:   "new tab left open for too long is treated as forbidden",
			frames: []frame{{0, chromeNew}, {10 * sec, chromeNew}},
			want:   []string{"warn 3"},
		},
		{
			name:   "typing an address restarts the new tab grace",
			frames: []frame{{0, chromeNew}, {9 * sec, chromeOK}, {sec, chromeNew}, {9 * sec, chromeNew}},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			if tt.cfg != nil {
				tt.cfg(&cfg)
			}
			w := &world{closeOK: tt.closeOK}
			play(newGuard(t, w, cfg), w, tt.frames)
			if !slices.Equal(w.events, tt.want) {
				t.Errorf("events:\n got %q\nwant %q", w.events, tt.want)
			}
		})
	}
}

func TestAllowedIsReportedOncePerSwitch(t *testing.T) {
	explorer := Window{ID: 5, Exe: "explorer.exe", Title: "Загрузки"}
	const sec = time.Second

	tests := []struct {
		name   string
		frames []frame
		want   []string
	}{
		{
			name:   "same window is reported once",
			frames: []frame{{0, chromeOK}, {sec, chromeOK}, {sec, chromeOK}},
			want:   []string{"chrome.exe"},
		},
		{
			name:   "each switch between allowed windows is reported",
			frames: []frame{{0, chromeOK}, {sec, explorer}, {sec, chromeOK}},
			want:   []string{"chrome.exe", "explorer.exe", "chrome.exe"},
		},
		{
			name:   "returning from the task screen is reported again",
			frames: []frame{{0, chromeOK}, {sec, overlay}, {sec, chromeOK}},
			want:   []string{"chrome.exe", "chrome.exe"},
		},
		{
			name:   "new tab counts as allowed while its grace lasts",
			frames: []frame{{0, chromeNew}, {sec, chromeNew}},
			want:   []string{"chrome.exe"},
		},
		{
			name:   "forbidden windows are never reported",
			frames: []frame{{0, telegram}, {sec, chromeBad}, {sec, overlay}},
			want:   nil,
		},
		{
			name:   "allowed tab after a forbidden one is reported",
			frames: []frame{{0, chromeOK}, {sec, chromeBad}, {sec, chromeOK}},
			want:   []string{"chrome.exe", "chrome.exe"},
		},
		{
			name:   "moment without a foreground window changes nothing",
			frames: []frame{{0, chromeOK}, {sec, Window{}}, {sec, chromeOK}},
			want:   []string{"chrome.exe"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &world{}
			play(newGuard(t, w, testConfig()), w, tt.frames)
			if !slices.Equal(w.passed, tt.want) {
				t.Errorf("passed = %q, want %q", w.passed, tt.want)
			}
		})
	}
}

func TestStepFailsOpen(t *testing.T) {
	t.Run("no rules", func(t *testing.T) {
		w := &world{}
		g := newGuard(t, w, testConfig())
		g.SetRules(nil)
		play(g, w, []frame{{0, telegram}, {time.Second, chromeBad}})
		if len(w.events) != 0 {
			t.Errorf("events = %q, want none", w.events)
		}
	})

	t.Run("foreground lookup fails", func(t *testing.T) {
		w := &world{winErr: errors.New("access denied")}
		play(newGuard(t, w, testConfig()), w, []frame{{0, telegram}})
		if len(w.events) != 0 {
			t.Errorf("events = %q, want none", w.events)
		}
	})

	t.Run("disabling rules clears the warning", func(t *testing.T) {
		w := &world{}
		g := newGuard(t, w, testConfig())
		play(g, w, []frame{{0, chromeBad}})
		g.SetRules(nil)
		if want := []string{"warn 3", "warn 0"}; !slices.Equal(w.events, want) {
			t.Errorf("events = %q, want %q", w.events, want)
		}
	})
}

func TestCloseTabErrorFallsBackToMinimize(t *testing.T) {
	cfg := testConfig()
	cfg.Countdown = 0
	w := &world{closeErr: errors.New("input is blocked")}
	play(newGuard(t, w, cfg), w, []frame{{0, chromeBad}})
	if want := []string{"minimize 1", "blocked chrome.exe (tabs)"}; !slices.Equal(w.events, want) {
		t.Errorf("events = %q, want %q", w.events, want)
	}
}

func TestGrace(t *testing.T) {
	cfg := testConfig()
	cfg.Countdown = 0
	w := &world{closeOK: true}
	g := newGuard(t, w, cfg)

	play(g, w, []frame{{0, chromeBad}, {time.Second, chromeBad}})
	g.Grace(start.Add(10 * time.Second))
	w.events = nil

	w.win = chromeBad
	g.Step(start.Add(5 * time.Second))
	if len(w.events) != 0 {
		t.Fatalf("events during grace = %q, want none", w.events)
	}

	w.win = telegram
	g.Step(start.Add(6 * time.Second))
	if want := []string{"minimize 2", "blocked telegram.exe (app)"}; !slices.Equal(w.events, want) {
		t.Fatalf("grace must not cover forbidden programs: events = %q, want %q", w.events, want)
	}
	w.events = nil

	w.win = chromeBad
	g.Step(start.Add(10 * time.Second))
	if want := []string{"close tab 1"}; !slices.Equal(w.events, want) {
		t.Errorf("events after grace = %q, want %q", w.events, want)
	}
}

func TestDefaultConfig(t *testing.T) {
	tests := []struct {
		countdown  time.Duration
		wantWindow time.Duration
	}{
		{countdown: 5 * time.Second, wantWindow: time.Minute},
		{countdown: 30 * time.Second, wantWindow: 2 * time.Minute},
	}
	for _, tt := range tests {
		cfg := DefaultConfig(tt.countdown)
		if cfg.Countdown != tt.countdown || cfg.StrikeWindow != tt.wantWindow || cfg.MaxStrikes != 3 {
			t.Errorf("DefaultConfig(%v) = %+v, want window %v", tt.countdown, cfg, tt.wantWindow)
		}
	}
}
