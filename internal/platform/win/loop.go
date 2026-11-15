//go:build windows

package win

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procShowWindow             = user32.NewProc("ShowWindow")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procSetWindowLongPtrW      = user32.NewProc("SetWindowLongPtrW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procSendMessageW           = user32.NewProc("SendMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procLoadCursorW            = user32.NewProc("LoadCursorW")
	procRegisterHotKey         = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey       = user32.NewProc("UnregisterHotKey")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop       = user32.NewProc("BringWindowToTop")
	procAttachThreadInput      = user32.NewProc("AttachThreadInput")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procMonitorFromPoint       = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW        = user32.NewProc("GetMonitorInfoW")
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
	procSetProcessDpiAwareness = user32.NewProc("SetProcessDpiAwarenessContext")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procCreateIconFromResource = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procFindWindowW            = user32.NewProc("FindWindowW")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procReleaseCapture         = user32.NewProc("ReleaseCapture")
	procIsZoomed               = user32.NewProc("IsZoomed")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procDwmSetWindowAttribute  = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wmNCCalcSize    = 0x0083
	wmNCLButtonDown = 0x00A1
	htCaption       = 2
	htTop           = 12
	sizeMinimized   = 1
	sizeMaximized   = 2

	swMaximize = 3
	swRestore  = 9

	smCySizeFrame    = 33
	smCxPaddedBorder = 92

	dwmwaDarkMode         = 20
	dwmwaCornerPreference = 33
	dwmwaBorderColor      = 34
	dwmCornersDefault     = 0
	dwmCornersSquare      = 1
	dwmCornersRound       = 2

	wmMove          = 0x0003
	wmSize          = 0x0005
	wmActivate      = 0x0006
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmSettingChange = 0x001A
	wmSetIcon       = 0x0080
	wmHotkey        = 0x0312
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmApp           = 0x8000

	wmRun    = wmApp + 1
	wmTray   = wmApp + 2
	wmWakeUp = wmApp + 3

	wsPopup            = 0x80000000
	wsOverlappedWindow = 0x00CF0000
	wsClipChildren     = 0x02000000
	wsExAppWindow      = 0x00040000
	wsExNoActivate     = 0x08000000

	gwlStyle = ^uintptr(15)

	swHide           = 0
	swShow           = 5
	swShowNoActivate = 4

	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	hwndTopmost   = ^uintptr(0)
	hwndNoTopmost = ^uintptr(1)

	idcArrow              = 32512
	monitorDefaultPrimary = 1
	dpiPerMonitorV2       = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	baseDPI               = 96
	smCxSmIcon            = 49
)

type point struct{ x, y int32 }

type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) Width() int32 { return r.Right - r.Left }

func (r Rect) Height() int32 { return r.Bottom - r.Top }

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type Loop struct {
	instance uintptr
	class    *uint16
	hwnd     uintptr
	taskbar  uint32

	mu    sync.Mutex
	queue []func()

	windows map[uintptr]*WebWindow
	hotkeys map[int]func()
	tray    *Tray
	onTheme func()
	onWake  func()
}

func NewLoop(className string, background uint32) (*Loop, error) {
	procSetProcessDpiAwareness.Call(dpiPerMonitorV2)

	l := &Loop{windows: map[uintptr]*WebWindow{}, hotkeys: map[int]func(){}}

	var err error
	if l.class, err = windows.UTF16PtrFromString(className); err != nil {
		return nil, fmt.Errorf("window class name: %w", err)
	}
	l.instance, _, _ = procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	brush, _, _ := procCreateSolidBrush.Call(uintptr(background))

	wc := wndClassEx{
		wndProc:    windows.NewCallback(l.proc),
		instance:   l.instance,
		cursor:     cursor,
		background: brush,
		className:  l.class,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		return nil, fmt.Errorf("register window class: %w", callErr)
	}

	hwnd, _, callErr := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(l.class)), 0, 0,
		0, 0, 0, 0, 0, 0, l.instance, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("create service window: %w", callErr)
	}
	l.hwnd = hwnd

	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	id, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	l.taskbar = uint32(id)
	return l, nil
}

func (l *Loop) Run() {
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (l *Loop) Post(fn func()) {
	l.mu.Lock()
	l.queue = append(l.queue, fn)
	l.mu.Unlock()
	procPostMessageW.Call(l.hwnd, wmRun, 0, 0)
}

// Quit завершает цикл сообщений.
func (l *Loop) Quit() {
	l.Post(func() {
		if l.tray != nil {
			l.tray.remove()
		}
		procPostQuitMessage.Call(0)
	})
}

func (l *Loop) OnThemeChange(fn func()) { l.onTheme = fn }

func (l *Loop) OnWake(fn func()) { l.onWake = fn }

func WakeRunningInstance(className string) bool {
	name, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(name)), 0)
	if hwnd == 0 {
		return false
	}
	ok, _, _ := procPostMessageW.Call(hwnd, wmWakeUp, 0, 0)
	return ok != 0
}

func (l *Loop) RegisterHotkey(id int, combo string, fn func()) error {
	mods, vk, err := ParseHotkey(combo)
	if err != nil {
		return err
	}
	l.UnregisterHotkey(id)
	if ok, _, callErr := procRegisterHotKey.Call(l.hwnd, uintptr(id), uintptr(mods|modNoRepeat), uintptr(vk)); ok == 0 {
		return fmt.Errorf("register hotkey %s: %w", combo, callErr)
	}
	l.hotkeys[id] = fn
	return nil
}

func (l *Loop) UnregisterHotkey(id int) {
	if _, ok := l.hotkeys[id]; ok {
		procUnregisterHotKey.Call(l.hwnd, uintptr(id))
		delete(l.hotkeys, id)
	}
}

func (l *Loop) proc(hwnd, message, wParam, lParam uintptr) uintptr {
	if hwnd == l.hwnd {
		switch uint32(message) {
		case wmRun:
			l.drain()
			return 0
		case wmHotkey:
			if fn := l.hotkeys[int(wParam)]; fn != nil {
				fn()
			}
			return 0
		case wmTray:
			if l.tray != nil {
				l.tray.handle(uint32(lParam))
			}
			return 0
		case wmWakeUp:
			if l.onWake != nil {
				l.onWake()
			}
			return 0
		case wmSettingChange:
			if l.onTheme != nil && lParam != 0 && systemString(lParam) == "ImmersiveColorSet" {
				l.onTheme()
			}
		case l.taskbar:
			if l.tray != nil {
				l.tray.add()
			}
			return 0
		}
	} else if w := l.windows[hwnd]; w != nil {
		if res, handled := w.handle(uint32(message), wParam, lParam); handled {
			return res
		}
	}
	res, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return res
}

func systemString(param uintptr) string {
	return windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&param)))
}

func (l *Loop) drain() {
	for {
		l.mu.Lock()
		queue := l.queue
		l.queue = nil
		l.mu.Unlock()
		if len(queue) == 0 {
			return
		}
		for _, fn := range queue {
			fn()
		}
	}
}

func WorkArea() (work, full Rect, err error) {
	monitor, _, _ := procMonitorFromPoint.Call(0, monitorDefaultPrimary)
	info := struct {
		size    uint32
		monitor Rect
		work    Rect
		flags   uint32
	}{}
	info.size = uint32(unsafe.Sizeof(info))
	if ok, _, callErr := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return Rect{}, Rect{}, fmt.Errorf("monitor info: %w", callErr)
	}
	return info.work, info.monitor, nil
}

func Scale(px int) int32 {
	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		dpi = baseDPI
	}
	return int32(px * int(dpi) / baseDPI)
}

func SmallIconSize() int {
	n, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	if n == 0 {
		return 16
	}
	return int(n)
}

// iconFromPNG создаёт значок Windows из картинки PNG.
func iconFromPNG(data []byte, size int) (uintptr, error) {
	if len(data) == 0 {
		return 0, errors.New("empty icon data")
	}
	const iconVersion = 0x00030000
	icon, _, callErr := procCreateIconFromResource.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		1, iconVersion, uintptr(size), uintptr(size), 0)
	if icon == 0 {
		return 0, fmt.Errorf("create icon: %w", callErr)
	}
	return icon, nil
}

func forceForeground(hwnd uintptr) {
	if ok, _, _ := procSetForegroundWindow.Call(hwnd); ok != 0 && foregroundWindow() == hwnd {
		return
	}
	fg := foregroundWindow()
	if fg == 0 || fg == hwnd {
		return
	}
	var pid uint32
	other, _, _ := procGetWindowThreadProcessID.Call(fg, uintptr(unsafe.Pointer(&pid)))
	self := uintptr(windows.GetCurrentThreadId())
	if other == 0 || other == self {
		return
	}
	if ok, _, _ := procAttachThreadInput.Call(self, other, 1); ok == 0 {
		return
	}
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procAttachThreadInput.Call(self, other, 0)
}
