package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"dofirst/internal/catalog"
	"dofirst/internal/domain"
	"dofirst/internal/service/core"
	"dofirst/internal/service/session"
	"dofirst/internal/service/stats"
)

// Режимы окна-сцены
const (
	ModeHidden  = "hidden"
	ModeOverlay = "overlay"
	ModeWidget  = "widget"
	ModeDialog  = "dialog"
)

// ядро приложения
type Backend interface {
	View() core.View
	Settings() domain.Settings
	UpdateSettings(s domain.Settings) (core.View, error)
	Activate(id string) (core.View, error)
	Pause(paused bool) (core.View, error)
	Complete(id, note string) (core.View, error)
	Disable(mode domain.DisableMode, reason string) (core.View, error)
	SnoozeNight(d time.Duration) (core.View, error)
	StartTest() (core.View, error)
	Launched()
	Journal() []domain.JournalEntry
	History() ([]stats.Day, []domain.DisableEvent)
	FirstLaunch() bool
}

type Shell interface {
	StageMode() string
	SetStageMode(mode string)
	PanelTab() string
	PendingUpdate() *UpdateInfo
	OfferUpdate(info UpdateInfo)
	DismissUpdate()
	Open(target string) error
	InstalledPrograms() []Program
	OpenPrograms() []Program
	ApplySettings(old, updated domain.Settings) error
	Quit()
}

type Program struct {
	Name  string `json:"name"`
	Exe   string `json:"exe"`
	Title string `json:"title,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type UpdateInfo struct {
	Configured bool   `json:"configured"`
	Version    string `json:"version,omitempty"`
}

type Updates interface {
	Check(ctx context.Context) (UpdateInfo, error)
	Apply(ctx context.Context) error
}

var (
	ErrHotkey = errors.New("hotkey is not available")
	errLaunch = errors.New("launch failed")
	errParams = errors.New("bad parameters")
)

type Bridge struct {
	core    Backend
	shell   Shell
	updates Updates
	version string
	log     *slog.Logger
	methods map[string]method
}

type method func(ctx context.Context, window string, params json.RawMessage) (any, error)

func NewBridge(backend Backend, shell Shell, updates Updates, version string, log *slog.Logger) *Bridge {
	b := &Bridge{core: backend, shell: shell, updates: updates, version: version, log: log}
	b.methods = map[string]method{
		"boot":          b.boot,
		"state":         b.state,
		"task.activate": b.taskActivate,
		"task.pause":    b.taskPause,
		"task.complete": b.taskComplete,
		"task.launch":   b.taskLaunch,
		"lock.disable":  b.lockDisable,
		"lock.test":     b.lockTest,
		"night.snooze":  b.nightSnooze,
		"stage.mode":    b.stageMode,
		"settings.get":  b.settingsGet,
		"settings.save": b.settingsSave,
		"apps.list":     b.appsList,
		"journal.list":  b.journalList,
		"stats.get":     b.statsGet,
		"update.check":  b.updateCheck,
		"update.apply":  b.updateApply,
		"update.later":  b.updateLater,
		"client.error":  b.clientError,
	}
	return b
}

type request struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type reply struct {
	Result any        `json:"result,omitempty"`
	Error  *callError `json:"error,omitempty"`
}

type callError struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (b *Bridge) Handle(ctx context.Context, window, raw string) string {
	var req request
	if err := json.Unmarshal([]byte(raw), &req); err != nil || req.ID == 0 {
		return ""
	}
	result, err := b.call(ctx, window, req)
	out := reply{Result: result}
	if err != nil {
		out = reply{Error: describe(err)}
	}
	data, err := json.Marshal(out)
	if err != nil {
		data, _ = json.Marshal(reply{Error: &callError{Code: "internal", Message: err.Error()}})
	}
	return fmt.Sprintf("window.dofirst.resolve(%d,%s)", req.ID, data)
}

func (b *Bridge) call(ctx context.Context, window string, req request) (any, error) {
	m, ok := b.methods[req.Method]
	if !ok {
		return nil, fmt.Errorf("%w: unknown method %q", errParams, req.Method)
	}
	return m(ctx, window, req.Params)
}

func describe(err error) *callError {
	code := "internal"
	switch {
	case errors.Is(err, session.ErrTimerRunning):
		code = "timer"
	case errors.Is(err, session.ErrNoteRequired):
		code = "note"
	case errors.Is(err, session.ErrOutOfOrder):
		code = "order"
	case errors.Is(err, ErrHotkey):
		code = "hotkey"
	case errors.Is(err, errLaunch):
		code = "launch"
	}
	return &callError{Code: code, Message: err.Error()}
}

func decode(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %w", errParams, err)
	}
	return nil
}

// ---------- общие вызовы ----------------------------------------------------

func (b *Bridge) boot(_ context.Context, window string, _ json.RawMessage) (any, error) {
	s := b.core.Settings()
	return map[string]any{
		"window":      window,
		"lang":        s.Language,
		"hotkey":      s.Hotkey,
		"version":     b.version,
		"mode":        b.shell.StageMode(),
		"view":        b.core.View(),
		"firstLaunch": b.core.FirstLaunch(),
		"tab":         b.shell.PanelTab(),
		"update":      b.shell.PendingUpdate(),
		"theme":       s.ActiveTheme(),
		"themes":      domain.BuiltinThemes(),
	}, nil
}

func (b *Bridge) clientError(_ context.Context, window string, raw json.RawMessage) (any, error) {
	var p struct {
		Message string `json:"message"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	b.log.Warn("page error", "window", window, "message", p.Message)
	return nil, nil
}

func (b *Bridge) state(context.Context, string, json.RawMessage) (any, error) {
	return b.core.View(), nil
}

func (b *Bridge) taskActivate(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return b.core.Activate(p.ID)
}

func (b *Bridge) taskPause(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		Paused bool `json:"paused"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return b.core.Pause(p.Paused)
}

func (b *Bridge) taskComplete(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		ID   string `json:"id"`
		Note string `json:"note"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	return b.core.Complete(p.ID, p.Note)
}

func (b *Bridge) taskLaunch(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		ID    string `json:"id"`
		Index int    `json:"index"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	view := b.core.View()
	for _, task := range view.Tasks {
		if task.ID != p.ID || task.Status != session.StatusActive {
			continue
		}
		if p.Index < 0 || p.Index >= len(task.Launch) {
			break
		}

		b.core.Launched()
		if err := b.shell.Open(task.Launch[p.Index]); err != nil {
			return nil, fmt.Errorf("%w: %w", errLaunch, err)
		}
		b.shell.SetStageMode(ModeWidget)
		return view, nil
	}
	return nil, fmt.Errorf("%w: no such launch target", errParams)
}

func (b *Bridge) lockDisable(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		Mode   domain.DisableMode `json:"mode"`
		Reason string             `json:"reason"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	switch p.Mode {
	case domain.DisableUntilTomorrow, domain.DisableNight, domain.DisableQuit:
	default:
		return nil, fmt.Errorf("%w: unknown disable mode %q", errParams, p.Mode)
	}
	view, err := b.core.Disable(p.Mode, p.Reason)
	if p.Mode == domain.DisableQuit {
		b.shell.Quit()
	}
	return view, err
}

func (b *Bridge) nightSnooze(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		Minutes int `json:"minutes"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Minutes < 1 || p.Minutes > 120 {
		return nil, fmt.Errorf("%w: snooze of %d minutes", errParams, p.Minutes)
	}
	return b.core.SnoozeNight(time.Duration(p.Minutes) * time.Minute)
}

func (b *Bridge) stageMode(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		Mode string `json:"mode"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	switch p.Mode {
	case ModeOverlay, ModeWidget:
	case ModeHidden:
		if b.core.View().Phase != "free" {
			return nil, fmt.Errorf("%w: lock is active", errParams)
		}
	default:
		return nil, fmt.Errorf("%w: unknown stage mode %q", errParams, p.Mode)
	}
	b.shell.SetStageMode(p.Mode)
	return nil, nil
}

// ---------- настройки, журнал, статистика ---------------------------------------

func (b *Bridge) settingsGet(context.Context, string, json.RawMessage) (any, error) {
	return map[string]any{"settings": b.core.Settings()}, nil
}

func (b *Bridge) settingsSave(_ context.Context, _ string, raw json.RawMessage) (any, error) {
	var p struct {
		Settings domain.Settings `json:"settings"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	old := b.core.Settings()
	if _, err := b.core.UpdateSettings(p.Settings); err != nil {
		return nil, err
	}
	if err := b.shell.ApplySettings(old, p.Settings); err != nil {
		reverted := p.Settings
		reverted.Hotkey = old.Hotkey
		if _, saveErr := b.core.UpdateSettings(reverted); saveErr != nil {
			return nil, errors.Join(err, saveErr)
		}
		return nil, err
	}
	return map[string]any{"settings": b.core.Settings()}, nil
}

func (b *Bridge) appsList(context.Context, string, json.RawMessage) (any, error) {
	installed := b.shell.InstalledPrograms()
	open := b.shell.OpenPrograms()

	icons := make(map[string]string)
	for _, p := range slices.Concat(installed, open) {
		if _, seen := icons[p.Exe]; !seen || icons[p.Exe] == "" {
			icons[p.Exe] = p.Icon
		}
	}

	type catalogApp struct {
		catalog.App
		Installed bool   `json:"installed"`
		Icon      string `json:"icon,omitempty"`
	}
	apps := make([]catalogApp, 0, len(catalog.Apps()))
	for _, app := range catalog.Apps() {
		item := catalogApp{App: app}
		for _, exe := range app.Exes {
			if icon, found := icons[exe]; found {
				item.Installed = true
				if item.Icon == "" {
					item.Icon = icon
				}
			}
		}
		apps = append(apps, item)
	}

	type localApp struct {
		Program
		Category catalog.Category `json:"category"`
	}
	classify := func(list []Program) []localApp {
		out := make([]localApp, 0, len(list))
		for _, p := range list {
			item := localApp{Program: p, Category: catalog.Other}
			if app, known := catalog.Find(p.Exe); known {
				item.Category = app.Category
			}
			out = append(out, item)
		}
		return out
	}

	return map[string]any{
		"categories": catalog.Categories(),
		"apps":       apps,
		"sites":      catalog.Sites(),
		"installed":  classify(installed),
		"open":       classify(open),
	}, nil
}

func (b *Bridge) journalList(context.Context, string, json.RawMessage) (any, error) {
	return b.core.Journal(), nil
}

func (b *Bridge) statsGet(context.Context, string, json.RawMessage) (any, error) {
	days, disables := b.core.History()
	return stats.Summarize(days, disables, b.core.View().Cycle), nil
}

func (b *Bridge) lockTest(context.Context, string, json.RawMessage) (any, error) {
	return b.core.StartTest()
}

// ---------- обновления ------------------------------------------------------------

func (b *Bridge) updateCheck(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
	info, err := b.updates.Check(ctx)
	if err != nil {
		return nil, err
	}
	if info.Version != "" {
		b.shell.OfferUpdate(info)
	}
	return info, nil
}

func (b *Bridge) updateApply(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
	if err := b.updates.Apply(ctx); err != nil {
		return nil, err
	}
	b.shell.Quit()
	return nil, nil
}

func (b *Bridge) updateLater(context.Context, string, json.RawMessage) (any, error) {
	b.shell.DismissUpdate()
	return nil, nil
}
