//go:build windows

package win

import (
	"errors"
	"fmt"
	"unsafe"
)

type Controller struct{}

func (Controller) Minimize(id uintptr) error {
	if ok, _, _ := procShowWindowAsync.Call(id, swMinimize); ok == 0 {
		return errors.New("window refused to minimize")
	}
	return nil
}

func (Controller) CloseTab(id uintptr) (bool, error) {
	if foregroundWindow() != id {
		return false, nil
	}
	for _, vk := range []uintptr{vkShift, vkMenu, vkLWin, vkRWin} {
		if keyDown(vk) {
			return false, nil
		}
	}

	var keys []keyInput
	held := keyDown(vkControl)
	if !held {
		keys = append(keys, press(vkControl))
	}
	keys = append(keys, press(vkW), release(vkW))
	if !held {
		keys = append(keys, release(vkControl))
	}

	sent, _, err := procSendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&keys[0])), unsafe.Sizeof(keys[0]))
	if int(sent) != len(keys) {
		return false, fmt.Errorf("send Ctrl+W: %d of %d keys accepted: %w", sent, len(keys), err)
	}
	return true, nil
}

type keyInput struct {
	kind  uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
	_     [8]byte
}

func press(vk uint16) keyInput   { return keyInput{kind: inputKeyboard, vk: vk} }
func release(vk uint16) keyInput { return keyInput{kind: inputKeyboard, vk: vk, flags: keyEventKeyUp} }
