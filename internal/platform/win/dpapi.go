//go:build windows

package win

import (
	"bytes"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type DPAPI struct {
	entropy []byte
}

func NewDPAPI(entropy string) DPAPI {
	return DPAPI{entropy: []byte(entropy)}
}

// Protect шифрует данные для текущего пользователя.
func (d DPAPI) Protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("dpapi protect: empty input")
	}
	var out windows.DataBlob
	err := windows.CryptProtectData(blob(plain), nil, blob(d.entropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, fmt.Errorf("dpapi protect: %w", err)
	}
	return take(out), nil
}

func (d DPAPI) Unprotect(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("dpapi unprotect: empty input")
	}
	var out windows.DataBlob
	err := windows.CryptUnprotectData(blob(sealed), nil, blob(d.entropy), 0, nil,
		windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return nil, fmt.Errorf("dpapi unprotect: %w", err)
	}
	return take(out), nil
}

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return nil
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func take(out windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return bytes.Clone(unsafe.Slice(out.Data, out.Size))
}
