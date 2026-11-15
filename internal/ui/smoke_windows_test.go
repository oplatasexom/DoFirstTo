//go:build windows

package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"dofirst/internal/domain"
	"dofirst/internal/platform/win"
	"dofirst/internal/service/core"
	"dofirst/internal/service/guard"
	"dofirst/internal/storage"
)

func TestMain(m *testing.M) {
	if os.Getenv("DOFIRST_UI_SMOKE") == "" {
		os.Exit(m.Run())
	}
	if err := smoke(); err != nil {
		fmt.Println("SMOKE FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("SMOKE OK")
}

type emptyDesk struct{}

func (emptyDesk) Foreground() (guard.Window, error) { return guard.Window{}, nil }
func (emptyDesk) Minimize(uintptr) error            { return nil }
func (emptyDesk) CloseTab(uintptr) (bool, error)    { return false, nil }

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var (
	smokeUser32          = windows.NewLazySystemDLL("user32.dll")
	smokeGetWindowRect   = smokeUser32.NewProc("GetWindowRect")
	smokeIsWindowVisible = smokeUser32.NewProc("IsWindowVisible")
	smokeGetClientRect   = smokeUser32.NewProc("GetClientRect")
	smokeSetCursorPos    = smokeUser32.NewProc("SetCursorPos")
	smokeSendInput       = smokeUser32.NewProc("SendInput")
)

func smoke() error {
	dir, err := os.MkdirTemp("", "dofirst-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	var logs syncBuffer
	log.SetOutput(&logs)
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	vault, err := storage.Open(filepath.Join(dir, "data"), win.NewDPAPI("DoFirstTo/smoke"))
	if err != nil {
		return err
	}
	loop, err := win.NewLoop("DoFirstTo.smoke", 0x0e1923)
	if err != nil {
		return err
	}
	app := New(Options{
		Loop: loop, Log: logger, Inspector: win.NewInspector(),
		WebViewDir: filepath.Join(dir, "webview"), Version: "smoke", Tick: 400 * time.Millisecond,
	})
	c, err := core.New(core.Deps{
		Store: vault, Inspector: emptyDesk{}, Controller: emptyDesk{},
		Screen: app, Now: time.Now, Log: logger,
	})
	if err != nil {
		return err
	}
	app.Attach(c, NoUpdates{})
	if _, err := c.StartTest(); err != nil {
		return err
	}

	result := make(chan error, 1)
	go func() {
		result <- smokeScript(app)
		app.Quit()
	}()
	go func() {
		time.Sleep(90 * time.Second)
		fmt.Println("SMOKE FAIL: timeout\n" + logs.String())
		os.Exit(3)
	}()

	app.Run(false)

	if err := <-result; err != nil {
		return fmt.Errorf("%w\n--- log ---\n%s", err, logs.String())
	}
	if out := logs.String(); strings.Contains(out, "page error") || strings.Contains(out, "level=ERROR") {
		return fmt.Errorf("errors in the log:\n%s", out)
	}
	return nil
}

func onUI[T any](a *App, fn func() T) T {
	ch := make(chan T, 1)
	a.loop.Post(func() { ch <- fn() })
	return <-ch
}

func waitUI(a *App, what string, cond func() bool) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if onUI(a, cond) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timed out waiting for " + what)
}

func windowRect(hwnd uintptr) (win.Rect, bool) {
	var r win.Rect
	smokeGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	visible, _, _ := smokeIsWindowVisible.Call(hwnd)
	return r, visible != 0
}

func smokeScript(a *App) error {
	work, full, err := win.WorkArea()
	if err != nil {
		return err
	}

	if err := waitUI(a, "task screen", func() bool { return a.stageReady && a.StageMode() == ModeOverlay }); err != nil {
		return err
	}
	stage := onUI(a, func() uintptr { return a.stage.Handle() })
	if r, visible := windowRect(stage); !visible || r != full {
		return fmt.Errorf("task screen: rect %+v visible=%v, want %+v", r, visible, full)
	}
	time.Sleep(2 * time.Second)

	js := a.bridge.Handle(context.Background(), WindowStage, `{"id":1,"method":"task.complete","params":{"id":"teeth","note":""}}`)
	if !strings.Contains(js, `"active":"study"`) {
		return fmt.Errorf("task.complete reply: %s", js)
	}

	started := time.Now()
	js = a.bridge.Handle(context.Background(), WindowPanel, `{"id":2,"method":"apps.list","params":{}}`)
	if !strings.Contains(js, `"apps":[`) || !strings.Contains(js, `"sites":[`) {
		return fmt.Errorf("apps.list reply has no catalog (%d bytes)", len(js))
	}
	if len(a.InstalledPrograms()) > 0 && !strings.Contains(js, "data:image/png;base64,") {
		return fmt.Errorf("apps.list found programs but extracted no icons (%d bytes)", len(js))
	}
	elapsed := time.Since(started).Round(time.Millisecond)
	var listed struct {
		Installed []Program `json:"installed"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(js, "window.dofirst.resolve(2,"), ")")
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return fmt.Errorf("apps.list reply: %w", err)
	}
	if err := json.Unmarshal(envelope.Result, &listed); err != nil {
		return fmt.Errorf("apps.list result: %w", err)
	}
	programs, bare := listed.Installed, 0
	for _, p := range programs {
		if p.Icon == "" {
			bare++
		}
	}
	fmt.Printf("apps.list: %d KB in %v; %d programs, %d without an icon\n", len(js)/1024, elapsed, len(programs), bare)
	if bare > len(programs)/10 {
		return fmt.Errorf("%d of %d programs have no icon", bare, len(programs))
	}

	a.SetStageMode(ModeWidget)
	if err := waitUI(a, "widget", func() bool { return a.StageMode() == ModeWidget && a.stage.Visible() }); err != nil {
		return err
	}
	r, visible := windowRect(stage)
	if !visible || r.Width() != win.Scale(340) || r.Height() != win.Scale(92) || r.Right > work.Right || r.Bottom > work.Bottom {
		return fmt.Errorf("widget: rect %+v visible=%v inside work area %+v", r, visible, work)
	}
	time.Sleep(2 * time.Second)

	a.loop.Post(func() { a.showPanel("tasks") })
	if err := waitUI(a, "settings window", func() bool { return a.panelReady && a.panel.Visible() }); err != nil {
		return err
	}
	panel := onUI(a, func() uintptr { return a.panel.Handle() })
	if r, visible := windowRect(panel); !visible || r.Width() < win.Scale(600) {
		return fmt.Errorf("settings window: rect %+v visible=%v", r, visible)
	}
	outer, _ := windowRect(panel)
	var client win.Rect
	smokeGetClientRect.Call(panel, uintptr(unsafe.Pointer(&client)))
	if lost := outer.Height() - client.Height(); client.Height() == 0 || lost > win.Scale(16) {
		return fmt.Errorf("settings window still has a system caption: window %d, client %d", outer.Height(), client.Height())
	}
	time.Sleep(1500 * time.Millisecond)

	press := func(method string) {
		a.loop.Post(func() { a.onMessage(WindowPanel, `{"id":90,"method":"`+method+`","params":{}}`) })
	}
	press("window.maximize")
	if err := waitUI(a, "maximized settings window", func() bool { return a.panel.Maximized() }); err != nil {
		return err
	}
	if r, _ := windowRect(panel); r.Width() < work.Width() {
		return fmt.Errorf("maximized window is %d wide, work area is %d", r.Width(), work.Width())
	}
	time.Sleep(800 * time.Millisecond)
	press("window.maximize")
	if err := waitUI(a, "restored settings window", func() bool { return !a.panel.Maximized() }); err != nil {
		return err
	}

	a.loop.Post(a.showMenu)
	if err := waitUI(a, "tray menu", func() bool { return a.menuReady && a.menu != nil && a.menu.Visible() }); err != nil {
		return err
	}
	time.Sleep(1500 * time.Millisecond)
	stillOpen := onUI(a, func() bool { return a.menu.Visible() })
	menu := onUI(a, func() uintptr { return a.menu.Handle() })
	r, visible = windowRect(menu)
	height := onUI(a, func() int32 { return a.menuHeight })
	if !stillOpen || !visible || r.Width() != win.Scale(292) || r.Height() != height || height < win.Scale(150) {
		return fmt.Errorf("tray menu: open=%v visible=%v rect %+v, reported height %d", stillOpen, visible, r, height)
	}
	if r.Right > work.Right || r.Bottom > work.Bottom || r.Left < work.Left || r.Top < work.Top {
		return fmt.Errorf("tray menu %+v is outside the work area %+v", r, work)
	}
	a.loop.Post(func() { a.onMessage(WindowMenu, `{"id":91,"method":"menu.act","params":{"id":"settings"}}`) })
	if err := waitUI(a, "tray menu to close", func() bool { return !a.menu.Visible() && a.panel.Visible() }); err != nil {
		return err
	}
	time.Sleep(800 * time.Millisecond)
	a.loop.Post(a.showMenu)
	if err := waitUI(a, "tray menu again", func() bool { return a.menu.Visible() }); err != nil {
		return err
	}
	time.Sleep(700 * time.Millisecond)
	a.loop.Post(func() { a.showPanel("") })
	if err := waitUI(a, "tray menu to close when another window is activated", func() bool { return !a.menu.Visible() }); err != nil {
		return err
	}
	time.Sleep(800 * time.Millisecond)
	if onUI(a, func() string { return a.StageMode() }) != ModeWidget {
		return errors.New("the widget is not on screen for the click test")
	}
	homeX, homeY := win.CursorPos()
	defer smokeSetCursorPos.Call(uintptr(homeX), uintptr(homeY))
	smokeSetCursorPos.Call(uintptr(work.Left+work.Width()/2), uintptr(work.Top+work.Height()/2))
	a.loop.Post(a.showMenu)
	if err := waitUI(a, "tray menu for the click test", func() bool { return a.menu.Visible() }); err != nil {
		return err
	}
	time.Sleep(600 * time.Millisecond)
	if !onUI(a, func() bool { return a.menu.Visible() }) {
		return errors.New("tray menu closed before anything was clicked")
	}
	widget, _ := windowRect(stage)
	smokeSetCursorPos.Call(uintptr(widget.Left+widget.Width()/2), uintptr(widget.Top+widget.Height()/2))
	time.Sleep(150 * time.Millisecond)
	const leftDown, leftUp = 0x0002, 0x0004
	smokeMouse(leftDown)
	err = waitUI(a, "tray menu to close on a press outside it", func() bool { return !a.menu.Visible() })
	smokeMouse(leftUp)
	if err != nil {
		return err
	}
	time.Sleep(800 * time.Millisecond)
	s := a.core.Settings()
	s.Theme = "mint"
	s.Themes = []domain.Theme{{ID: "th-smoke", Name: "Проверка", Background: "#0b1622", Accent: "#2ec4b6", Text: "#e8f1f2"}}
	params, err := json.Marshal(map[string]any{"settings": s})
	if err != nil {
		return err
	}
	js = a.bridge.Handle(context.Background(), WindowPanel, `{"id":3,"method":"settings.save","params":`+string(params)+`}`)
	if !strings.Contains(js, `"theme":"mint"`) {
		return fmt.Errorf("settings.save did not keep the theme: %s", js)
	}
	if got := a.core.Settings().ActiveTheme(); got.ID != "mint" || !got.Light() {
		return fmt.Errorf("active theme = %+v, want the light mint", got)
	}
	a.loop.Post(func() { a.showPanel("themes") })
	time.Sleep(1500 * time.Millisecond)
	a.loop.Post(func() { a.setMode(ModeOverlay) })
	time.Sleep(1500 * time.Millisecond)
	return nil
}

func smokeMouse(flags uint32) {
	type mouseInput struct {
		kind              uint32
		_                 uint32
		dx, dy            int32
		data, flags, time uint32
		_                 uint32
		extra             uintptr
	}
	in := mouseInput{flags: flags}
	smokeSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}
