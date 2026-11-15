//go:build windows

package win

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString    = 0x000
	mfGrayed    = 0x001
	mfSeparator = 0x800

	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100
)

type notifyIconData struct {
	size            uint32
	hwnd            uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	version         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guid            windows.GUID
	balloonIcon     uintptr
}

type MenuItem struct {
	Text      string
	Disabled  bool
	Separator bool
	Do        func()
}

type Tray struct {
	loop    *Loop
	data    notifyIconData
	onClick func()
	onMenu  func()
}

func (l *Loop) NewTray(tooltip string, icon []byte, onClick, onMenu func()) (*Tray, error) {
	t := &Tray{loop: l, onClick: onClick, onMenu: onMenu}
	t.data = notifyIconData{hwnd: l.hwnd, id: 1, flags: nifMessage | nifIcon | nifTip, callbackMessage: wmTray}
	t.data.size = uint32(unsafe.Sizeof(t.data))
	t.setTip(tooltip)

	handle, err := iconFromPNG(icon, SmallIconSize())
	if err != nil {
		return nil, err
	}
	t.data.icon = handle
	if !t.add() {
		return nil, fmt.Errorf("add tray icon: shell refused")
	}
	l.tray = t
	return t, nil
}

func (t *Tray) SetIcon(icon []byte) error {
	handle, err := iconFromPNG(icon, SmallIconSize())
	if err != nil {
		return err
	}
	old := t.data.icon
	t.data.icon = handle
	t.notify(nimModify)
	if old != 0 {
		procDestroyIcon.Call(old)
	}
	return nil
}

func (t *Tray) SetTooltip(text string) {
	t.setTip(text)
	t.notify(nimModify)
}

func (t *Tray) setTip(text string) {
	tip := windows.StringToUTF16(text)
	if len(tip) > len(t.data.tip) {
		tip = append(tip[:len(t.data.tip)-1], 0)
	}
	t.data.tip = [128]uint16{}
	copy(t.data.tip[:], tip)
}

func (t *Tray) add() bool { return t.notify(nimAdd) }

func (t *Tray) remove() { t.notify(nimDelete) }

func (t *Tray) notify(action uintptr) bool {
	ok, _, _ := procShellNotifyIconW.Call(action, uintptr(unsafe.Pointer(&t.data)))
	return ok != 0
}

func (t *Tray) handle(event uint32) {
	switch event {
	case wmLButtonUp:
		if t.onClick != nil {
			t.onClick()
		}
	case wmRButtonUp:
		if t.onMenu != nil {
			t.onMenu()
		}
	}
}

func (t *Tray) PopupMenu(items []MenuItem) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu) 

	for i, item := range items {
		if item.Separator {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		text, err := windows.UTF16PtrFromString(item.Text)
		if err != nil {
			continue
		}
		flags := uintptr(mfString)
		if item.Disabled {
			flags |= mfGrayed
		}
		procAppendMenuW.Call(menu, flags, uintptr(i+1), uintptr(unsafe.Pointer(text)))
	}

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	procSetForegroundWindow.Call(t.loop.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmNoNotify|tpmRightButton|tpmBottomAlign,
		uintptr(cursor.x), uintptr(cursor.y), 0, t.loop.hwnd, 0)
	procPostMessageW.Call(t.loop.hwnd, 0, 0, 0)

	if i := int(cmd) - 1; i >= 0 && i < len(items) && items[i].Do != nil {
		items[i].Do()
	}
}
