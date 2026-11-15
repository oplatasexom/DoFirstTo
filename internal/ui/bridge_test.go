package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"dofirst/internal/domain"
	"dofirst/internal/service/core"
	"dofirst/internal/service/session"
	"dofirst/internal/service/stats"
)

type fakeCore struct {
	view     core.View
	settings domain.Settings
	err      error
	calls    []string
	saved    []domain.Settings
}

func (f *fakeCore) note(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeCore) View() core.View           { return f.view }
func (f *fakeCore) Settings() domain.Settings { return f.settings }
func (f *fakeCore) FirstLaunch() bool         { return true }
func (f *fakeCore) Launched()                 { f.note("launched") }
func (f *fakeCore) Journal() []domain.JournalEntry {
	return []domain.JournalEntry{{Title: "Учёба", Note: "каналы"}}
}

func (f *fakeCore) History() ([]stats.Day, []domain.DisableEvent) {
	at := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	return []stats.Day{{Cycle: "2026-11-10", LockedAt: at, DoneAt: at.Add(time.Hour)}}, nil
}

func (f *fakeCore) UpdateSettings(s domain.Settings) (core.View, error) {
	if f.err != nil {
		return f.view, f.err
	}
	f.settings = s
	f.saved = append(f.saved, s)
	return f.view, nil
}

func (f *fakeCore) Activate(id string) (core.View, error) {
	f.note("activate %s", id)
	return f.view, f.err
}

func (f *fakeCore) Pause(paused bool) (core.View, error) {
	f.note("pause %v", paused)
	return f.view, f.err
}

func (f *fakeCore) Complete(id, note string) (core.View, error) {
	f.note("complete %s %q", id, note)
	return f.view, f.err
}

func (f *fakeCore) Disable(mode domain.DisableMode, reason string) (core.View, error) {
	f.note("disable %s %q", mode, reason)
	return f.view, f.err
}

func (f *fakeCore) SnoozeNight(d time.Duration) (core.View, error) {
	f.note("snooze %s", d)
	return f.view, f.err
}

func (f *fakeCore) StartTest() (core.View, error) {
	f.note("test")
	return f.view, f.err
}

type fakeShell struct {
	mode     string
	calls    []string
	openErr  error
	applyErr error
}

func (s *fakeShell) StageMode() string           { return s.mode }
func (s *fakeShell) SetStageMode(mode string)    { s.mode = mode; s.calls = append(s.calls, "mode "+mode) }
func (s *fakeShell) PanelTab() string            { return "tasks" }
func (s *fakeShell) PendingUpdate() *UpdateInfo  { return nil }
func (s *fakeShell) OfferUpdate(info UpdateInfo) { s.calls = append(s.calls, "offer "+info.Version) }
func (s *fakeShell) DismissUpdate()              { s.calls = append(s.calls, "dismiss") }
func (s *fakeShell) Quit()                       { s.calls = append(s.calls, "quit") }

func (s *fakeShell) Open(target string) error {
	s.calls = append(s.calls, "open "+target)
	return s.openErr
}

func (s *fakeShell) InstalledPrograms() []Program {
	return []Program{
		{Name: "Visual Studio Code", Exe: "code.exe", Icon: "data:image/png;base64,AAAA"},
		{Name: "qBittorrent", Exe: "qbittorrent.exe"},
	}
}

func (s *fakeShell) OpenPrograms() []Program {
	return []Program{
		{Name: "Zen", Exe: "zen.exe", Title: "roadmap.sh", Icon: "data:image/png;base64,BBBB"},
		{Name: "Code", Exe: "code.exe", Title: "main.go"},
	}
}

func (s *fakeShell) ApplySettings(_, updated domain.Settings) error {
	s.calls = append(s.calls, "apply "+updated.Hotkey)
	return s.applyErr
}

type fakeUpdates struct{ applyErr error }

func (fakeUpdates) Check(context.Context) (UpdateInfo, error) {
	return UpdateInfo{Configured: true, Version: "0.2.0"}, nil
}
func (u fakeUpdates) Apply(context.Context) error { return u.applyErr }

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type rig struct {
	core   *fakeCore
	shell  *fakeShell
	bridge *Bridge
}

func newRig() *rig {
	r := &rig{
		core: &fakeCore{
			settings: domain.DefaultSettings(),
			view: core.View{Phase: "locked", Active: "study", Tasks: []core.TaskView{
				{ID: "teeth", Status: session.StatusDone, Launch: []string{"https://done.example"}},
				{ID: "study", Status: session.StatusActive, Launch: []string{"https://roadmap.sh", `D:\book.pdf`}},
			}},
		},
		shell: &fakeShell{mode: ModeOverlay},
	}
	r.bridge = NewBridge(r.core, r.shell, fakeUpdates{}, "1.2.3", quietLog())
	return r
}

func (r *rig) invoke(t *testing.T, method string, params any) (json.RawMessage, string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": 7, "method": method, "params": params})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	js := r.bridge.Handle(context.Background(), WindowStage, string(raw))
	body, ok := strings.CutPrefix(js, "window.dofirst.resolve(7,")
	if !ok || !strings.HasSuffix(body, ")") {
		t.Fatalf("unexpected reply: %s", js)
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *callError      `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(body, ")")), &out); err != nil {
		t.Fatalf("reply is not JSON: %v\n%s", err, js)
	}
	if out.Error != nil {
		return nil, out.Error.Code
	}
	return out.Result, ""
}

func TestHandleIgnoresGarbage(t *testing.T) {
	r := newRig()
	for _, raw := range []string{"", "not json", `{"method":"state"}`, `{"id":0,"method":"state"}`} {
		if js := r.bridge.Handle(context.Background(), WindowStage, raw); js != "" {
			t.Errorf("Handle(%q) = %q, want empty", raw, js)
		}
	}
	if _, code := r.invoke(t, "no.such.method", nil); code != "internal" {
		t.Errorf("unknown method: code = %q, want internal", code)
	}
}

func TestBoot(t *testing.T) {
	r := newRig()
	raw, code := r.invoke(t, "boot", nil)
	if code != "" {
		t.Fatalf("boot failed: %s", code)
	}
	var boot struct {
		Window, Lang, Hotkey, Version, Mode, Tab string
		FirstLaunch                              bool
		View                                     core.View
		Theme                                    domain.Theme
		Themes                                   []domain.Theme
	}
	if err := json.Unmarshal(raw, &boot); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if boot.Window != WindowStage || boot.Lang != "ru" || boot.Hotkey != "Ctrl+Alt+D" || boot.Version != "1.2.3" ||
		boot.Mode != ModeOverlay || boot.Tab != "tasks" || !boot.FirstLaunch || boot.View.Active != "study" {
		t.Errorf("boot = %+v", boot)
	}
	if boot.Theme.ID != domain.DefaultThemeID || len(boot.Themes) != len(domain.BuiltinThemes()) {
		t.Errorf("boot theme = %q with %d built-in themes", boot.Theme.ID, len(boot.Themes))
	}
}

func TestErrorCodes(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("complete: %w", session.ErrTimerRunning), "timer"},
		{fmt.Errorf("complete: %w", session.ErrNoteRequired), "note"},
		{fmt.Errorf("complete: %w", session.ErrOutOfOrder), "order"},
		{errors.New("disk failure"), "internal"},
	}
	for _, tt := range tests {
		r := newRig()
		r.core.err = tt.err
		if _, code := r.invoke(t, "task.complete", map[string]any{"id": "study", "note": "x"}); code != tt.want {
			t.Errorf("%v: code = %q, want %q", tt.err, code, tt.want)
		}
	}
}

func TestTaskActions(t *testing.T) {
	r := newRig()
	r.invoke(t, "task.activate", map[string]any{"id": "study"})
	r.invoke(t, "task.pause", map[string]any{"paused": true})
	r.invoke(t, "task.pause", map[string]any{"paused": false})
	r.invoke(t, "task.complete", map[string]any{"id": "study", "note": "каналы"})
	r.invoke(t, "night.snooze", map[string]any{"minutes": 15})
	want := []string{"activate study", "pause true", "pause false", `complete study "каналы"`, "snooze 15m0s"}
	if !slices.Equal(r.core.calls, want) {
		t.Errorf("calls = %q, want %q", r.core.calls, want)
	}
	if _, code := r.invoke(t, "night.snooze", map[string]any{"minutes": 100000}); code != "internal" {
		t.Errorf("huge snooze: code = %q, want internal", code)
	}
	if _, code := r.invoke(t, "task.complete", "not an object"); code != "internal" {
		t.Errorf("bad params: code = %q, want internal", code)
	}
}

func TestTaskLaunch(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		index     int
		openErr   error
		wantCode  string
		wantShell []string
	}{
		{name: "target of the active task", id: "study", index: 1, wantShell: []string{`open D:\book.pdf`, "mode widget"}},
		{name: "index out of range", id: "study", index: 2, wantCode: "internal"},
		{name: "negative index", id: "study", index: -1, wantCode: "internal"},
		{name: "task is not active", id: "teeth", index: 0, wantCode: "internal"},
		{name: "unknown task", id: "ghost", index: 0, wantCode: "internal"},
		{name: "shell fails", id: "study", index: 0, openErr: errors.New("no handler"), wantCode: "launch", wantShell: []string{"open https://roadmap.sh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig()
			r.shell.openErr = tt.openErr
			_, code := r.invoke(t, "task.launch", map[string]any{"id": tt.id, "index": tt.index})
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if !slices.Equal(r.shell.calls, tt.wantShell) {
				t.Errorf("shell calls = %q, want %q", r.shell.calls, tt.wantShell)
			}
			if launched := slices.Contains(r.core.calls, "launched"); launched != (len(tt.wantShell) > 0) {
				t.Errorf("grace started = %v, want %v", launched, len(tt.wantShell) > 0)
			}
		})
	}
}

func TestStageMode(t *testing.T) {
	tests := []struct {
		phase, mode string
		wantCode    string
	}{
		{"locked", ModeWidget, ""},
		{"locked", ModeOverlay, ""},
		{"locked", ModeHidden, "internal"},
		{"night", ModeHidden, "internal"},
		{"free", ModeHidden, ""},
		{"locked", ModeDialog, "internal"},
		{"locked", "fullscreen", "internal"},
	}
	for _, tt := range tests {
		r := newRig()
		r.core.view.Phase = tt.phase
		_, code := r.invoke(t, "stage.mode", map[string]any{"mode": tt.mode})
		if code != tt.wantCode {
			t.Errorf("%s → %s: code = %q, want %q", tt.phase, tt.mode, code, tt.wantCode)
		}
		if changed := r.shell.mode == tt.mode; changed != (tt.wantCode == "") && tt.mode != ModeOverlay {
			t.Errorf("%s → %s: mode is now %q", tt.phase, tt.mode, r.shell.mode)
		}
	}
}

func TestLockDisable(t *testing.T) {
	tests := []struct {
		mode     string
		wantCode string
		wantQuit bool
	}{
		{mode: "tomorrow"},
		{mode: "night"},
		{mode: "quit", wantQuit: true},
		{mode: "forever", wantCode: "internal"},
	}
	for _, tt := range tests {
		r := newRig()
		_, code := r.invoke(t, "lock.disable", map[string]any{"mode": tt.mode, "reason": "встреча"})
		if code != tt.wantCode {
			t.Errorf("%s: code = %q, want %q", tt.mode, code, tt.wantCode)
		}
		if quit := slices.Contains(r.shell.calls, "quit"); quit != tt.wantQuit {
			t.Errorf("%s: quit = %v, want %v", tt.mode, quit, tt.wantQuit)
		}
		if called := len(r.core.calls) == 1; called != (tt.wantCode == "") {
			t.Errorf("%s: core calls = %q", tt.mode, r.core.calls)
		}
	}
}

func TestSettingsSave(t *testing.T) {
	t.Run("applies and returns saved settings", func(t *testing.T) {
		r := newRig()
		s := domain.DefaultSettings()
		s.Hotkey = "Ctrl+Alt+K"
		raw, code := r.invoke(t, "settings.save", map[string]any{"settings": s})
		if code != "" || !strings.Contains(string(raw), "Ctrl+Alt+K") {
			t.Fatalf("settings.save = %s, %q", raw, code)
		}
		if !slices.Equal(r.shell.calls, []string{"apply Ctrl+Alt+K"}) {
			t.Errorf("shell calls = %q", r.shell.calls)
		}
	})

	t.Run("rejected settings are not applied", func(t *testing.T) {
		r := newRig()
		r.core.err = errors.New("task #1: empty title")
		if _, code := r.invoke(t, "settings.save", map[string]any{"settings": domain.DefaultSettings()}); code != "internal" {
			t.Errorf("code = %q, want internal", code)
		}
		if len(r.shell.calls) != 0 {
			t.Errorf("shell was touched: %q", r.shell.calls)
		}
	})

	t.Run("busy hotkey is rolled back", func(t *testing.T) {
		r := newRig()
		r.shell.applyErr = ErrHotkey
		s := domain.DefaultSettings()
		s.Hotkey = "Ctrl+Alt+Delete"
		s.TabCountdownSec = 9
		if _, code := r.invoke(t, "settings.save", map[string]any{"settings": s}); code != "hotkey" {
			t.Fatalf("code = %q, want hotkey", code)
		}
		if got := r.core.settings; got.Hotkey != "Ctrl+Alt+D" || got.TabCountdownSec != 9 {
			t.Errorf("settings = hotkey %q, countdown %d; want old hotkey and new countdown", got.Hotkey, got.TabCountdownSec)
		}
	})
}

func TestReadOnlyLists(t *testing.T) {
	r := newRig()
	checks := map[string]string{
		"journal.list": "каналы",
		"stats.get":    `"doneDays":1`,
		"settings.get": `"hotkey":"Ctrl+Alt+D"`,
		"update.check": `"version":"0.2.0"`,
	}
	for method, want := range checks {
		raw, code := r.invoke(t, method, nil)
		if code != "" || !strings.Contains(string(raw), want) {
			t.Errorf("%s = %s (%q), want it to contain %s", method, raw, code, want)
		}
	}
	if _, code := r.invoke(t, "lock.test", nil); code != "" || !slices.Contains(r.core.calls, "test") {
		t.Errorf("lock.test: code %q, calls %q", code, r.core.calls)
	}
}

func TestUpdates(t *testing.T) {
	r := newRig()
	if _, code := r.invoke(t, "update.apply", nil); code != "" || !slices.Contains(r.shell.calls, "quit") {
		t.Errorf("successful update must restart: code %q, shell %q", code, r.shell.calls)
	}

	r = newRig()
	r.bridge = NewBridge(r.core, r.shell, fakeUpdates{applyErr: errors.New("bad signature")}, "1.2.3", quietLog())
	if _, code := r.invoke(t, "update.apply", nil); code != "internal" || slices.Contains(r.shell.calls, "quit") {
		t.Errorf("failed update must not quit: code %q, shell %q", code, r.shell.calls)
	}

	r.invoke(t, "update.later", nil)
	if !slices.Contains(r.shell.calls, "dismiss") {
		t.Errorf("update.later did not dismiss: %q", r.shell.calls)
	}
}

func TestAppsList(t *testing.T) {
	r := newRig()
	raw, code := r.invoke(t, "apps.list", nil)
	if code != "" {
		t.Fatalf("apps.list failed: %s", code)
	}
	var got struct {
		Categories []string
		Apps       []struct {
			ID, Name, Category, Icon string
			Exes                     []string
			Installed                bool
		}
		Sites []struct {
			ID     string
			Titles []string
		}
		Installed, Open []struct{ Name, Exe, Category, Title, Icon string }
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	byID := map[string]int{}
	for i, app := range got.Apps {
		byID[app.ID] = i
	}
	if app := got.Apps[byID["vscode"]]; !app.Installed || app.Icon != "data:image/png;base64,AAAA" {
		t.Errorf("vscode = %+v, want installed with the Start menu icon", app)
	}
	if app := got.Apps[byID["zen"]]; !app.Installed || app.Icon != "data:image/png;base64,BBBB" {
		t.Errorf("zen = %+v, want installed with the window icon", app)
	}
	if app := got.Apps[byID["spotify"]]; app.Installed || app.Icon != "" || len(app.Exes) == 0 {
		t.Errorf("spotify = %+v, want not installed, but with its program names", app)
	}

	if len(got.Installed) != 2 || got.Installed[0].Category != "dev" || got.Installed[1].Category != "other" {
		t.Errorf("installed = %+v, want the editor in dev and the unknown program in other", got.Installed)
	}
	if len(got.Open) != 2 || got.Open[0].Category != "browsers" || got.Open[0].Title != "roadmap.sh" {
		t.Errorf("open = %+v, want the browser first with its title", got.Open)
	}
	if len(got.Sites) == 0 || len(got.Categories) == 0 {
		t.Errorf("sites = %d, categories = %d; want both filled", len(got.Sites), len(got.Categories))
	}
}

func TestI18n(t *testing.T) {
	raw, err := assets.ReadFile("web/i18n.js")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entry := regexp.MustCompile(`^    '([a-z0-9.]+)': '((?:[^'\\]|\\.)*)',$`)
	keys := map[string]map[string]bool{}
	lang := ""
	for i, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		switch strings.TrimSpace(line) {
		case "ru: {", "en: {":
			lang = strings.TrimSpace(line)[:2]
			keys[lang] = map[string]bool{}
			continue
		}
		if lang == "" || !strings.HasPrefix(line, "    '") {
			continue
		}
		m := entry.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("line %d is not a complete entry: %s", i+1, line)
			continue
		}
		if keys[lang][m[1]] {
			t.Errorf("line %d: duplicate key %q in %s", i+1, m[1], lang)
		}
		if strings.TrimSpace(m[2]) == "" {
			t.Errorf("line %d: empty text for %q", i+1, m[1])
		}
		keys[lang][m[1]] = true
	}
	if len(keys["ru"]) < 50 {
		t.Fatalf("only %d Russian keys found: the dictionary was not parsed", len(keys["ru"]))
	}
	for key := range keys["ru"] {
		if !keys["en"][key] {
			t.Errorf("key %q has no English text", key)
		}
	}
	for key := range keys["en"] {
		if !keys["ru"][key] {
			t.Errorf("key %q has no Russian text", key)
		}
	}
	for _, th := range domain.BuiltinThemes() {
		if !keys["ru"]["theme."+th.ID] {
			t.Errorf("built-in theme %q has no name in the dictionary", th.ID)
		}
	}
}

func TestPage(t *testing.T) {
	for _, window := range []string{WindowStage, WindowPanel} {
		page, err := Page(window, "window.__extra=1;")
		if err != nil {
			t.Fatalf("Page(%s): %v", window, err)
		}
		for _, want := range []string{
			fmt.Sprintf("window.__dofirstWindow=%q", window), "window.__extra=1;",
			"DOFIRST_I18N", "const DF =", "--accent-text", "<div id=\"root\">",
		} {
			if !strings.Contains(page, want) {
				t.Errorf("Page(%s) does not contain %q", window, want)
			}
		}
		for _, marker := range []string{"/*CSS*/", "/*BOOT*/", "/*JS*/"} {
			if strings.Contains(page, marker) {
				t.Errorf("Page(%s) still contains the marker %s", window, marker)
			}
		}
		if n := strings.Count(page, "</script>"); n != 2 {
			t.Errorf("Page(%s) has %d closing script tags, want 2: a script body must not contain one", window, n)
		}
	}
}
