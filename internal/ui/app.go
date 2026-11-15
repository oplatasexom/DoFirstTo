//go:build windows

package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"dofirst/internal/domain"
	"dofirst/internal/logo"
	"dofirst/internal/platform/win"
	"dofirst/internal/service/core"
	"dofirst/internal/service/guard"
)

const (
	hotkeyID = 1
	collapseDelay = 1500 * time.Millisecond
	iconWorkers = 8
	celebrateFor = 20 * time.Second
	menuPoll = 30 * time.Millisecond
)

type Options struct {
	Loop      *win.Loop
	Log       *slog.Logger
	Inspector *win.Inspector
	Autostart *win.Autostart
	WebViewDir string
	Version    string
	Tick time.Duration
}

type App struct {
	opts   Options
	loop   *win.Loop
	log    *slog.Logger
	core   *core.Core
	bridge *Bridge
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	mode     string
	panelTab string
	update   *UpdateInfo
	lang     string
	icons    map[string]string

	stage, panel, menu     *win.WebWindow
	stageReady, panelReady bool
	menuReady              bool
	menuX, menuY           int32
	menuHeight             int32
	menuShows              int
	view                   core.View
	shownAt                time.Time
	celebrate              bool
	tray                   *win.Tray
}

func New(opts Options) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{opts: opts, loop: opts.Loop, log: opts.Log, ctx: ctx, cancel: cancel, mode: ModeHidden, lang: "ru", icons: map[string]string{}}
}

func (a *App) Attach(c *core.Core, updates Updates) {
	a.core = c
	a.bridge = NewBridge(c, a, updates, a.opts.Version, a.log)
	a.lang = c.Settings().Language
}

func (a *App) Run(showPanel bool) {
	a.loop.Post(func() {
		a.initTray()
		a.syncSystem()
		a.loop.OnWake(a.primary)
		a.loop.OnThemeChange(a.refreshTray)
		if showPanel {
			a.showPanel("")
		}
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		a.core.Run(a.ctx, a.opts.Tick)
	}()

	a.loop.Run()
	a.cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		a.log.Warn("core did not stop in time")
	}
}

// ---------- core.Screen ------------------

func (a *App) Changed(v core.View) {
	a.loop.Post(func() { a.apply(v) })
}

func (a *App) Blocked(w guard.Window, reason guard.Reason) {
	a.loop.Post(func() {
		if !a.locked() {
			return
		}
		a.setMode(ModeOverlay)
		kind := "app"
		if reason == guard.ReasonTabs {
			kind = "tabs"
		}
		a.emitStage("blocked", map[string]string{"app": appName(w.Exe), "reason": kind})
	})
}

func (a *App) TabWarning(_ guard.Window, seconds int) {
	a.loop.Post(func() { a.emitStage("tabWarning", map[string]int{"seconds": seconds}) })
}

func (a *App) Allowed(guard.Window) {
	a.loop.Post(func() {
		if a.StageMode() == ModeOverlay && a.locked() && !a.celebrate && time.Since(a.shownAt) > collapseDelay {
			a.setMode(ModeWidget)
		}
	})
}

func (a *App) apply(v core.View) {
	prev := a.view
	a.view = v
	mode := a.StageMode()

	switch {
	case a.locked() && (prev.Phase != v.Phase || prev.Cycle != v.Cycle || mode == ModeHidden || mode == ModeDialog):
		a.celebrate = false
		a.setMode(ModeOverlay)
	case !a.locked() && prev.Phase == "locked" && v.Reason == "done" && !prev.Test:
		a.celebrate = true
		a.setMode(ModeOverlay)
		time.AfterFunc(celebrateFor, func() {
			a.loop.Post(func() {
				if a.celebrate {
					a.celebrate = false
					a.setMode(ModeHidden)
				}
			})
		})
	case !a.locked() && !a.celebrate && mode != ModeHidden && mode != ModeDialog:
		a.setMode(ModeHidden)
	}

	a.emitStage("state", v)
	a.refreshTray()
}

func (a *App) locked() bool {
	return a.view.Phase == "locked" || a.view.Phase == "night"
}

// ---------- Shell вызывается из горутин моста ----------------------------------

func (a *App) StageMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mode
}

func (a *App) SetStageMode(mode string) {
	a.loop.Post(func() {
		if mode == ModeHidden {
			a.celebrate = false
		}
		a.setMode(mode)
	})
}

func (a *App) PanelTab() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.panelTab
}

func (a *App) PendingUpdate() *UpdateInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.update
}

func (a *App) DismissUpdate() {
	a.mu.Lock()
	a.update = nil
	a.mu.Unlock()
	a.loop.Post(func() {
		if a.StageMode() == ModeDialog {
			a.setMode(ModeHidden)
		}
	})
}

func (a *App) OfferUpdate(info UpdateInfo) {
	a.mu.Lock()
	a.update = &info
	a.mu.Unlock()
	a.loop.Post(func() {
		if a.StageMode() == ModeHidden {
			a.setMode(ModeDialog)
		}
		a.emitStage("update", info)
	})
}

func (a *App) Open(target string) error { return win.Open(target) }

func (a *App) InstalledPrograms() []Program {
	found := win.InstalledPrograms()
	paths := make([]string, len(found))
	for i, p := range found {
		paths[i] = p.IconPath
	}
	icons := a.iconsOf(paths)
	out := make([]Program, 0, len(found))
	for i, p := range found {
		out = append(out, Program{Name: p.Name, Exe: p.Exe, Icon: icons[i]})
	}
	return out
}

func (a *App) OpenPrograms() []Program {
	found := a.opts.Inspector.OpenPrograms()
	paths := make([]string, len(found))
	for i, p := range found {
		paths[i] = p.Path
	}
	icons := a.iconsOf(paths)
	out := make([]Program, 0, len(found))
	for i, p := range found {
		out = append(out, Program{Name: appName(p.Exe), Exe: p.Exe, Title: p.Title, Icon: icons[i]})
	}
	return out
}

func (a *App) iconsOf(paths []string) []string {
	out := make([]string, len(paths))
	if len(paths) == 0 {
		return out
	}
	out[0] = a.icon(paths[0])

	limit := make(chan struct{}, iconWorkers)
	var wg sync.WaitGroup
	for i, path := range paths[1:] {
		i++
		wg.Add(1)
		limit <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-limit }()
			out[i] = a.icon(path)
		}()
	}
	wg.Wait()
	return out
}

func (a *App) icon(path string) string {
	if path == "" {
		return ""
	}
	a.mu.Lock()
	cached, ok := a.icons[path]
	a.mu.Unlock()
	if ok {
		return cached
	}

	data, err := win.IconPNG(path)
	if err != nil {
		a.log.Debug("icon is unavailable", "err", err)
		return ""
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	a.mu.Lock()
	a.icons[path] = url
	a.mu.Unlock()
	return url
}

func (a *App) ApplySettings(old, updated domain.Settings) error {
	if a.opts.Autostart != nil && old.Autostart != updated.Autostart {
		if err := a.opts.Autostart.Set(updated.Autostart); err != nil {
			return err
		}
	}
	if old.Hotkey != updated.Hotkey {
		done := make(chan error, 1)
		a.loop.Post(func() { done <- a.loop.RegisterHotkey(hotkeyID, updated.Hotkey, a.toggle) })
		if err := <-done; err != nil {
			a.loop.Post(func() { _ = a.loop.RegisterHotkey(hotkeyID, old.Hotkey, a.toggle) })
			return fmt.Errorf("%w: %w", ErrHotkey, err)
		}
	}
	if old.Language != updated.Language {
		a.mu.Lock()
		a.lang = updated.Language
		a.mu.Unlock()
		a.loop.Post(func() {
			a.emitStage("lang", updated.Language)
			a.emitPanel("lang", updated.Language)
			a.emitMenu("lang", updated.Language)
			a.refreshTray()
		})
	}
	if theme := updated.ActiveTheme(); theme != old.ActiveTheme() {
		a.loop.Post(func() {
			r, g, b := theme.BackgroundRGB()
			for _, w := range []*win.WebWindow{a.stage, a.panel, a.menu} {
				if w != nil {
					w.SetColors(r, g, b, theme.Light())
				}
			}
			a.emitStage("theme", theme)
			a.emitPanel("theme", theme)
			a.emitMenu("theme", theme)
		})
	}
	return nil
}

// завершает приложение
func (a *App) Quit() {
	a.loop.Quit()
}

// ---------- поток интерфейса -------------------------------------------------------

func (a *App) language() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lang
}

func (a *App) syncSystem() {
	s := a.core.Settings()
	if err := a.loop.RegisterHotkey(hotkeyID, s.Hotkey, a.toggle); err != nil {
		a.log.Warn("hotkey is not registered", "hotkey", s.Hotkey, "err", err)
	}
	if as := a.opts.Autostart; as != nil {
		// Сверяем и саму команду: после переноса .exe запись нужно обновить.
		if on, err := as.Enabled(); err != nil || on != s.Autostart {
			if err := as.Set(s.Autostart); err != nil {
				a.log.Warn("autostart is not updated", "err", err)
			}
		}
	}
}

func (a *App) toggle() {
	switch {
	case !a.locked():
		a.showPanel("")
	case a.StageMode() == ModeOverlay:
		a.setMode(ModeWidget)
	default:
		a.setMode(ModeOverlay)
	}
}

func (a *App) primary() {
	if a.locked() {
		a.setMode(ModeOverlay)
		return
	}
	a.showPanel("")
}

func (a *App) setMode(mode string) {
	a.mu.Lock()
	a.mode = mode
	a.mu.Unlock()

	if mode == ModeHidden {
		if a.stage != nil {
			a.stage.Hide()
		}
		return
	}
	if !a.ensureStage() {
		return
	}
	work, full, err := win.WorkArea()
	if err != nil {
		a.log.Error("monitor is unavailable", "err", err)
		return
	}

	var p win.Placement
	switch mode {
	case ModeWidget:
		w, h, margin := win.Scale(340), win.Scale(92), win.Scale(16)
		p = win.Placement{X: work.Right - w - margin, Y: work.Bottom - h - margin, W: w, H: h, Topmost: true, NoActivate: true, Rounded: true}
	case ModeDialog:
		w, h := win.Scale(470), win.Scale(250)
		p = win.Placement{X: work.Left + (work.Width()-w)/2, Y: work.Top + (work.Height()-h)/2, W: w, H: h, Topmost: true}
	default:
		p = win.Placement{X: full.Left, Y: full.Top, W: full.Width(), H: full.Height(), Topmost: true}
		a.shownAt = time.Now()
	}
	a.stage.Show(p)
	a.emitStage("mode", mode)
}

func (a *App) ensureStage() bool {
	if a.stage != nil {
		return true
	}
	w, err := a.newWindow(WindowStage, "DoFirstTo", win.WebOptions{})
	if err != nil {
		a.log.Error("task screen is unavailable", "err", err)
		return false
	}
	a.stage = w
	return true
}

func (a *App) showPanel(tab string) {
	a.mu.Lock()
	if tab != "" {
		a.panelTab = tab
	}
	a.mu.Unlock()

	if a.StageMode() == ModeOverlay {
		if a.locked() {
			a.setMode(ModeWidget)
		} else {
			a.celebrate = false
			a.setMode(ModeHidden)
		}
	}

	if a.panel == nil {
		w, err := a.newWindow(WindowPanel, tr(a.language(), "panel.title"), win.WebOptions{
			OnClose: func() { a.panel.Hide() },
			OnState: func(maximized bool) { a.emitPanel("window", map[string]bool{"maximized": maximized}) },
		})
		if err != nil {
			a.log.Error("settings window is unavailable", "err", err)
			return
		}
		a.panel = w
	}

	work, _, err := win.WorkArea()
	if err != nil {
		a.log.Error("monitor is unavailable", "err", err)
		return
	}
	w := min(win.Scale(1100), work.Width()-win.Scale(40))
	h := min(win.Scale(780), work.Height()-win.Scale(40))
	a.panel.Show(win.Placement{X: work.Left + (work.Width()-w)/2, Y: work.Top + (work.Height()-h)/2, W: w, H: h, Frame: true})
	a.emitPanel("shown", nil)
	if tab != "" {
		a.emitPanel("tab", tab)
	}
}

func (a *App) newWindow(kind, title string, hooks win.WebOptions) (*win.WebWindow, error) {
	theme := a.core.Settings().ActiveTheme()
	themeJSON, err := json.Marshal(theme)
	if err != nil {
		return nil, fmt.Errorf("encode theme: %w", err)
	}
	page, err := Page(kind, fmt.Sprintf("window.__dofirstTheme=%s;", themeJSON))
	if err != nil {
		return nil, err
	}
	icon, err := logo.PNG(256, logo.App)
	if err != nil {
		return nil, err
	}
	opts := hooks
	opts.Title = title
	opts.DataPath = a.opts.WebViewDir
	opts.Icon = icon
	opts.BackgroundR, opts.BackgroundG, opts.BackgroundB = theme.BackgroundRGB()
	opts.Light = theme.Light()
	opts.OnMessage = func(message string) { a.onMessage(kind, message) }
	opts.OnError = func(err error) { a.log.Error("webview", "window", kind, "err", err) }

	w, err := a.loop.NewWebWindow(opts)
	if err != nil {
		return nil, err
	}
	w.LoadHTML(page)
	return w, nil
}

func (a *App) onMessage(kind, message string) {
	switch kind {
	case WindowStage:
		a.stageReady = true
	case WindowPanel:
		a.panelReady = true
	case WindowMenu:
		a.menuReady = true
	}
	if js, handled := a.windowCall(kind, message); handled {
		if w := a.window(kind); w != nil && js != "" {
			w.Eval(js)
		}
		return
	}
	go func() {
		js := a.bridge.Handle(a.ctx, kind, message)
		if js == "" {
			return
		}
		a.loop.Post(func() {
			if w := a.window(kind); w != nil {
				w.Eval(js)
			}
		})
	}()
}

func (a *App) window(kind string) *win.WebWindow {
	switch kind {
	case WindowStage:
		return a.stage
	case WindowMenu:
		return a.menu
	default:
		return a.panel
	}
}

func (a *App) windowCall(kind, message string) (string, bool) {
	var req request
	if err := json.Unmarshal([]byte(message), &req); err != nil || req.ID == 0 {
		return "", false
	}
	var params struct {
		Edge   string `json:"edge"`
		ID     string `json:"id"`
		Height int32  `json:"height"`
	}
	_ = json.Unmarshal(req.Params, &params)

	switch {
	case kind == WindowPanel && a.panel != nil && strings.HasPrefix(req.Method, "window."):
		switch req.Method {
		case "window.drag":
			a.panel.BeginDrag(params.Edge == "top")
		case "window.minimize":
			a.panel.Minimize()
		case "window.maximize":
			a.panel.ToggleMaximize()
		case "window.close":
			a.panel.Hide()
		}
	case kind == WindowMenu && req.Method == "menu.size":
		a.resizeMenu(params.Height)
	case kind == WindowMenu && req.Method == "menu.act":
		a.menuAction(params.ID)
	default:
		return "", false
	}
	return fmt.Sprintf("window.dofirst.resolve(%d,{})", req.ID), true
}

func (a *App) emitStage(event string, payload any) {
	if a.stage != nil && a.stageReady {
		a.emit(a.stage, event, payload)
	}
}

func (a *App) emitPanel(event string, payload any) {
	if a.panel != nil && a.panelReady {
		a.emit(a.panel, event, payload)
	}
}

func (a *App) emit(w *win.WebWindow, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		a.log.Error("encode event", "event", event, "err", err)
		return
	}
	w.Eval(fmt.Sprintf("window.dofirst&&window.dofirst.emit(%q,%s)", event, data))
}

// ---------- трей -----------------------------------------------------------------

func (a *App) trayIcon() ([]byte, error) {
	style := logo.TrayOnDark
	if win.TaskbarIsLight() {
		style = logo.TrayOnLight
	}
	return logo.PNG(win.SmallIconSize(), style)
}

func (a *App) initTray() {
	icon, err := a.trayIcon()
	if err != nil {
		a.log.Error("tray icon", "err", err)
		return
	}
	tray, err := a.loop.NewTray(tr(a.language(), "tip.free"), icon, a.primary, a.showMenu)
	if err != nil {
		a.log.Error("tray", "err", err)
		return
	}
	a.tray = tray
}

func (a *App) refreshTray() {
	if a.tray == nil {
		return
	}
	if icon, err := a.trayIcon(); err == nil {
		_ = a.tray.SetIcon(icon)
	}
	tip := "tip.free"
	switch a.view.Phase {
	case "locked":
		tip = "tip.locked"
	case "night":
		tip = "tip.night"
	}
	a.tray.SetTooltip(tr(a.language(), tip))
}

func (a *App) showMenu() {
	if a.menu == nil {
		w, err := a.newWindow(WindowMenu, "DoFirstTo", win.WebOptions{
			OnDeactivate: func() {
				a.loop.Post(func() {
					if a.menu.Visible() && win.ForegroundHandle() != a.menu.Handle() {
						a.menu.Hide()
					}
				})
			},
		})
		if err != nil {
			a.log.Error("tray menu window is unavailable", "err", err)
			a.tray.PopupMenu(a.trayMenu())
			return
		}
		a.menu = w
		a.menuHeight = win.Scale(232)
	}
	a.menuX, a.menuY = win.CursorPos()
	a.placeMenu()
	a.emitMenu("state", a.core.View())
	a.watchMenu()
}

func (a *App) watchMenu() {
	a.menuShows++
	show, watch := a.menuShows, &popupWatch{}
	go func() {
		ticker := time.NewTicker(menuPoll)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
			}
			open := make(chan bool, 1)
			a.loop.Post(func() { open <- a.pollMenu(show, watch) })
			select {
			case <-a.ctx.Done():
				return
			case still := <-open:
				if !still {
					return
				}
			}
		}
	}()
}

func (a *App) pollMenu(show int, watch *popupWatch) bool {
	if show != a.menuShows || a.menu == nil || !a.menu.Visible() {
		return false
	}
	x, y := win.CursorPos()
	probe := popupProbe{
		ButtonDown:   win.MouseButtonDown(),
		CursorInside: a.menu.Rect().Contains(x, y),
		Foreground:   win.ForegroundHandle(),
		Self:         a.menu.Handle(),
	}
	if watch.shouldClose(probe) {
		a.menu.Hide()
		return false
	}
	return true
}

func (a *App) placeMenu() {
	work, _, err := win.WorkArea()
	if err != nil {
		a.log.Error("monitor is unavailable", "err", err)
		return
	}
	w, h, gap := win.Scale(292), a.menuHeight, win.Scale(10)
	x := min(max(a.menuX-w+win.Scale(28), work.Left+gap), work.Right-w-gap)
	y := a.menuY - h - gap
	if y < work.Top+gap {
		y = min(a.menuY+gap, work.Bottom-h-gap)
	}
	a.menu.Show(win.Placement{X: x, Y: y, W: w, H: h, Topmost: true, Rounded: true})
}

func (a *App) resizeMenu(height int32) {
	height = min(max(height, win.Scale(96)), win.Scale(520))
	if height == a.menuHeight {
		return
	}
	a.menuHeight = height
	if a.menu != nil && a.menu.Visible() {
		a.placeMenu()
	}
}

func (a *App) menuAction(id string) {
	if a.menu != nil {
		a.menu.Hide()
	}
	switch id {
	case "show":
		a.setMode(ModeOverlay)
	case "pause", "resume":
		go func() {
			if _, err := a.core.Pause(id == "pause"); err != nil {
				a.log.Warn("pause from the tray menu", "err", err)
			}
		}()
	case "settings":
		a.showPanel("")
	case "disable":
		a.setMode(ModeOverlay)
		a.emitStage("askDisable", nil)
	case "quit":
		a.quitFromTray()
	}
}

func (a *App) quitFromTray() {
	if !a.locked() {
		a.Quit()
		return
	}
	go func() {
		if _, err := a.core.Disable(domain.DisableQuit, ""); err != nil {
			a.log.Error("record quit", "err", err)
		}
		a.Quit()
	}()
}

// пункты запасного системного меню
func (a *App) trayMenu() []win.MenuItem {
	lang := a.language()
	locked := a.locked()
	return []win.MenuItem{
		{Text: tr(lang, "tray.show"), Disabled: !locked, Do: func() { a.menuAction("show") }},
		{Text: tr(lang, "tray.settings"), Do: func() { a.menuAction("settings") }},
		{Separator: true},
		{Text: tr(lang, "tray.disable"), Disabled: !locked, Do: func() { a.menuAction("disable") }},
		{Text: tr(lang, "tray.quit"), Do: a.quitFromTray},
	}
}

func (a *App) emitMenu(event string, payload any) {
	if a.menu != nil && a.menuReady {
		a.emit(a.menu, event, payload)
	}
}

func appName(exe string) string {
	name := strings.TrimSuffix(exe, ".exe")
	if name == "" {
		return exe
	}
	return strings.ToUpper(name[:1]) + name[1:]
}
