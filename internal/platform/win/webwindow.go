//go:build windows

package win

import (
	"fmt"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

func WebViewVersion() (string, error) {
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil {
		return "", fmt.Errorf("webview2 runtime: %w", err)
	}
	if v == "" {
		return "", fmt.Errorf("webview2 runtime is not installed")
	}
	return v, nil
}

type WebOptions struct {
	Title                                 string
	DataPath                              string
	Icon                                  []byte
	BackgroundR, BackgroundG, BackgroundB uint8
	Light                                 bool
	OnMessage                             func(message string)
	OnClose                               func()
	OnDeactivate                          func()
	OnState                               func(maximized bool)
	OnError                               func(err error)
}

type Placement struct {
	X, Y, W, H int32
	Frame      bool
	Topmost    bool
	NoActivate bool
	Rounded    bool
}

type WebWindow struct {
	hwnd         uintptr
	web          *edge.Chromium
	onClose      func()
	onDeactivate func()
	onState      func(maximized bool)
	shown        bool
	frame        bool
	maximized    bool
}

func (l *Loop) NewWebWindow(o WebOptions) (*WebWindow, error) {
	title, err := windows.UTF16PtrFromString(o.Title)
	if err != nil {
		return nil, fmt.Errorf("window title: %w", err)
	}
	hwnd, _, callErr := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(l.class)), uintptr(unsafe.Pointer(title)),
		wsPopup|wsClipChildren, 0, 0, 320, 240, 0, 0, l.instance, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("create window: %w", callErr)
	}
	w := &WebWindow{hwnd: hwnd, onClose: o.OnClose, onDeactivate: o.OnDeactivate, onState: o.OnState}
	l.windows[hwnd] = w

	w.frameColors(o.BackgroundR, o.BackgroundG, o.BackgroundB, o.Light)

	if len(o.Icon) > 0 {
		for kind, size := range map[uintptr]int{0: SmallIconSize(), 1: 2 * SmallIconSize()} { // ICON_SMALL, ICON_BIG
			if icon, err := iconFromPNG(o.Icon, size); err == nil {
				procSendMessageW.Call(hwnd, wmSetIcon, kind, icon)
			}
		}
	}

	web := edge.NewChromium()
	web.DataPath = o.DataPath
	if o.OnError != nil {
		web.SetErrorCallback(o.OnError)
	}
	if o.OnMessage != nil {
		web.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
			o.OnMessage(message)
		}
	}
	web.Embed(hwnd)
	w.web = web

	if s, err := web.GetSettings(); err == nil {
		_ = s.PutAreDefaultContextMenusEnabled(false)
		_ = s.PutAreDevToolsEnabled(false)
		_ = s.PutIsStatusBarEnabled(false)
		_ = s.PutIsZoomControlEnabled(false)
		_ = s.PutAreBrowserAcceleratorKeysEnabled(false)
	}
	web.SetBackgroundColour(o.BackgroundR, o.BackgroundG, o.BackgroundB, 255)
	_ = web.Hide()
	return w, nil
}

func (w *WebWindow) LoadHTML(html string) { w.web.NavigateToString(html) }

func (w *WebWindow) Eval(js string) { w.web.Eval(js) }

func (w *WebWindow) SetColors(r, g, b uint8, light bool) {
	w.frameColors(r, g, b, light)
	w.web.SetBackgroundColour(r, g, b, 255)
}

func (w *WebWindow) frameColors(r, g, b uint8, light bool) {
	dark := uint32(1)
	if light {
		dark = 0
	}
	dwmSet(w.hwnd, dwmwaDarkMode, dark)
	dwmSet(w.hwnd, dwmwaBorderColor, uint32(b)<<16|uint32(g)<<8|uint32(r))
}

func (w *WebWindow) Visible() bool { return w.shown }

func (w *WebWindow) Rect() Rect {
	var r Rect
	procGetWindowRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func (w *WebWindow) Handle() uintptr { return w.hwnd }

func (w *WebWindow) Show(p Placement) {
	style := uintptr(wsPopup | wsClipChildren)
	exStyle := uintptr(wsExToolWindow)
	if p.Frame {
		style = wsOverlappedWindow | wsClipChildren
		exStyle = wsExAppWindow
	}
	if p.NoActivate {
		exStyle |= wsExNoActivate
	}
	w.frame = p.Frame
	procSetWindowLongPtrW.Call(w.hwnd, gwlStyle, style)
	procSetWindowLongPtrW.Call(w.hwnd, gwlExStyle, exStyle)

	corners := uint32(dwmCornersDefault)
	if !p.Frame {
		corners = dwmCornersSquare
		if p.Rounded {
			corners = dwmCornersRound
		}
	}
	dwmSet(w.hwnd, dwmwaCornerPreference, corners)

	after := hwndNoTopmost
	if p.Topmost {
		after = hwndTopmost
	}
	flags := uintptr(swpFrameChanged)
	show := uintptr(swShow)
	if p.NoActivate {
		flags |= swpNoActivate
		show = swShowNoActivate
	}
	procSetWindowPos.Call(w.hwnd, after, uintptr(p.X), uintptr(p.Y), uintptr(p.W), uintptr(p.H), flags)
	procShowWindow.Call(w.hwnd, show)
	w.shown = true

	_ = w.web.Show()
	w.web.Resize()
	if !p.NoActivate {
		forceForeground(w.hwnd)
		w.web.Focus()
	}
}

func (w *WebWindow) Hide() {
	if !w.shown {
		return
	}
	w.shown = false
	procShowWindow.Call(w.hwnd, swHide)
	_ = w.web.Hide()
}

func (w *WebWindow) BeginDrag(top bool) {
	if !w.frame {
		return
	}
	hit := uintptr(htCaption)
	if top {
		if w.maximized {
			return
		}
		hit = htTop
	}
	procReleaseCapture.Call()
	procPostMessageW.Call(w.hwnd, wmNCLButtonDown, hit, 0)
}

func (w *WebWindow) Minimize() {
	procShowWindow.Call(w.hwnd, swMinimize)
}

func (w *WebWindow) ToggleMaximize() {
	if !w.frame {
		return
	}
	cmd := uintptr(swMaximize)
	if w.Maximized() {
		cmd = swRestore
	}
	procShowWindow.Call(w.hwnd, cmd)
}

func (w *WebWindow) Maximized() bool {
	zoomed, _, _ := procIsZoomed.Call(w.hwnd)
	return zoomed != 0
}

func (w *WebWindow) handle(message uint32, wParam, lParam uintptr) (uintptr, bool) {
	if message == wmNCCalcSize && w.frame && wParam != 0 {
		return w.calcClientArea(wParam, lParam), true
	}
	if w.web == nil {
		return 0, false
	}
	switch message {
	case wmSize:
		w.web.Resize()
		if maximized := wParam == sizeMaximized; w.frame && wParam != sizeMinimized && maximized != w.maximized {
			w.maximized = maximized
			if w.onState != nil {
				w.onState(maximized)
			}
		}
	case wmMove:
		_ = w.web.NotifyParentWindowPositionChanged()
	case wmSetFocus:
		if w.shown {
			w.web.Focus()
		}
	case wmActivate:
		switch {
		case !w.shown:
		case wParam&0xffff != 0:
			w.web.Focus()
		case w.onDeactivate != nil:
			w.onDeactivate()
		}
	case wmClose:
		if w.onClose != nil {
			w.onClose()
		}
		return 0, true
	}
	return 0, false
}

func (w *WebWindow) calcClientArea(wParam, lParam uintptr) uintptr {
	rect := *(**Rect)(unsafe.Pointer(&lParam))
	top := rect.Top
	procDefWindowProcW.Call(w.hwnd, wmNCCalcSize, wParam, lParam)
	rect.Top = top
	if w.Maximized() {
		frame, _, _ := procGetSystemMetrics.Call(smCySizeFrame)
		padding, _, _ := procGetSystemMetrics.Call(smCxPaddedBorder)
		rect.Top += int32(frame + padding)
	}
	return 0
}

func dwmSet(hwnd uintptr, attribute uintptr, value uint32) {
	procDwmSetWindowAttribute.Call(hwnd, attribute, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
}

func ForegroundHandle() uintptr { return foregroundWindow() }

func MouseButtonDown() bool {
	const vkLButton, vkRButton, vkMButton = 0x01, 0x02, 0x04
	down := false
	for _, vk := range []uintptr{vkLButton, vkRButton, vkMButton} {
		state, _, _ := procGetAsyncKeyState.Call(vk)
		down = down || state&(keyPressedMask|keyPressedSince) != 0
	}
	return down
}

func (r Rect) Contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

func CursorPos() (x, y int32) {
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p.x, p.y
}
