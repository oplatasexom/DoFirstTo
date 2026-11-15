//go:build windows

package win

import (
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"dofirst/internal/service/guard"
)

type Inspector struct {
	self uint32

	mu sync.Mutex

	childCB  uintptr
	hostPID  uint32
	childPID uint32

	listCB uintptr
	list   []uintptr
}

func NewInspector() *Inspector {
	i := &Inspector{self: windows.GetCurrentProcessId()}
	i.childCB = windows.NewCallback(i.onChild)
	i.listCB = windows.NewCallback(i.onWindow)
	return i
}

func (i *Inspector) Foreground() (guard.Window, error) {
	hwnd := foregroundWindow()
	if hwnd == 0 {
		return guard.Window{}, nil
	}
	return i.describe(hwnd), nil
}

type OpenProgram struct {
	Exe string
	Path string
	Title string
}

func (i *Inspector) OpenPrograms() []OpenProgram {
	i.mu.Lock()
	i.list = i.list[:0]
	procEnumWindows.Call(i.listCB, 0) //nolint:errcheck
	handles := slices.Clone(i.list)
	i.mu.Unlock()

	var out []OpenProgram
	seen := make(map[string]struct{})
	for _, hwnd := range handles {
		w, path := i.describePath(hwnd)
		if w.Own || w.Exe == "" || strings.TrimSpace(w.Title) == "" {
			continue
		}
		if _, dup := seen[w.Exe]; dup {
			continue
		}
		seen[w.Exe] = struct{}{}
		out = append(out, OpenProgram{Exe: w.Exe, Path: path, Title: w.Title})
	}
	return out
}

func (i *Inspector) describe(hwnd uintptr) guard.Window {
	w, _ := i.describePath(hwnd)
	return w
}

func (i *Inspector) describePath(hwnd uintptr) (guard.Window, string) {
	pid := windowProcessID(hwnd)
	w := guard.Window{ID: hwnd, Title: windowText(hwnd), Own: pid == i.self}
	if pid == 0 || w.Own {
		return w, ""
	}
	path := exePath(pid)
	if baseName(path) == frameHostExe {
		path = ""
		if child := i.hostedPID(hwnd, pid); child != 0 {
			path = exePath(child)
		}
	}
	w.Exe = baseName(path)
	return w, path
}

// hostedPID ищет процесс настоящей программы внутри окна ApplicationFrameHost
func (i *Inspector) hostedPID(hwnd uintptr, hostPID uint32) uint32 {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.hostPID, i.childPID = hostPID, 0
	procEnumChildWindows.Call(hwnd, i.childCB, 0)
	return i.childPID
}

func (i *Inspector) onChild(hwnd, _ uintptr) uintptr {
	if pid := windowProcessID(hwnd); pid != 0 && pid != i.hostPID {
		i.childPID = pid
		return enumStop
	}
	return enumContinue
}

func (i *Inspector) onWindow(hwnd, _ uintptr) uintptr {
	if listable(hwnd) {
		i.list = append(i.list, hwnd)
	}
	return enumContinue
}

func listable(hwnd uintptr) bool {
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 {
		return false
	}
	if style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle); style&wsExToolWindow != 0 {
		return false
	}
	var cloaked uint32
	procDwmGetWindowAttribute.Call(hwnd, dwmwaCloaked,
		uintptr(unsafe.Pointer(&cloaked)), unsafe.Sizeof(cloaked))
	return cloaked == 0
}

func exeName(pid uint32) string {
	return baseName(exePath(pid))
}

func exePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, maxProcessPath)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(path))
}
