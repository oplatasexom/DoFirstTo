//go:build windows

package win

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")

	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procEnumChildWindows         = user32.NewProc("EnumChildWindows")
	procShowWindowAsync          = user32.NewProc("ShowWindowAsync")
	procSendInput                = user32.NewProc("SendInput")
	procGetAsyncKeyState         = user32.NewProc("GetAsyncKeyState")

	procDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	swMinimize = 6

	gwlExStyle      = ^uintptr(19) // -20
	wsExToolWindow  = 0x00000080
	dwmwaCloaked    = 14
	inputKeyboard   = 1
	keyEventKeyUp   = 0x0002
	vkShift         = 0x10
	vkControl       = 0x11
	vkMenu          = 0x12 // Alt
	vkLWin          = 0x5B
	vkRWin          = 0x5C
	vkW             = 0x57
	keyPressedMask  = 0x8000
	keyPressedSince = 0x0001
	maxProcessPath  = 1024
	frameHostExe    = "applicationframehost.exe"
	enumContinue    = 1
	enumStop        = 0
)

func foregroundWindow() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

func windowProcessID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	got, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:got])
}

func keyDown(vk uintptr) bool {
	state, _, _ := procGetAsyncKeyState.Call(vk)
	return state&keyPressedMask != 0
}
