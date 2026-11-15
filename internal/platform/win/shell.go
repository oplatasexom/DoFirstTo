//go:build windows

package win

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	vkSpace = 0x20
	vkF1    = 0x70

	MessageBoxError = 0x10
	MessageBoxInfo  = 0x40
)

func ParseHotkey(combo string) (mods, vk uint32, err error) {
	for _, part := range strings.Split(combo, "+") {
		key := strings.ToLower(strings.TrimSpace(part))
		switch key {
		case "":
			return 0, 0, fmt.Errorf("hotkey %q: empty part", combo)
		case "ctrl", "control":
			mods |= modControl
		case "alt":
			mods |= modAlt
		case "shift":
			mods |= modShift
		case "win":
			mods |= modWin
		default:
			if vk != 0 {
				return 0, 0, fmt.Errorf("hotkey %q: more than one key", combo)
			}
			if vk = keyCode(key); vk == 0 {
				return 0, 0, fmt.Errorf("hotkey %q: unknown key %q", combo, part)
			}
		}
	}
	switch {
	case vk == 0:
		return 0, 0, fmt.Errorf("hotkey %q: no key", combo)
	case mods == 0:
		return 0, 0, fmt.Errorf("hotkey %q: no modifier", combo)
	}
	return mods, vk, nil
}

func keyCode(key string) uint32 {
	if key == "space" {
		return vkSpace
	}
	if len(key) == 1 {
		switch c := key[0]; {
		case c >= 'a' && c <= 'z':
			return uint32(c - 'a' + 'A')
		case c >= '0' && c <= '9':
			return uint32(c)
		}
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(key, "f")); err == nil && key[0] == 'f' && n >= 1 && n <= 24 {
		return vkF1 + uint32(n) - 1
	}
	return 0
}

func Open(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("open %q: %w", target, err)
	}
	if err := windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("open %q: %w", target, err)
	}
	return nil
}

func AttachParentConsole() {
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	const attachParentProcess = ^uintptr(0)
	if ok, _, _ := kernel32.NewProc("AttachConsole").Call(attachParentProcess); ok == 0 {
		return
	}
	if console, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = console, console
	}
}

func MessageBox(title, text string, kind uint32) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, kind)
}
