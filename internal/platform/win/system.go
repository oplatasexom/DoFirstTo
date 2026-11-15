//go:build windows

package win

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const RunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type Autostart struct {
	Key     string
	Name    string
	Command string
}

func (a Autostart) Enabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, a.Key, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: open key: %w", err)
	}
	defer k.Close()

	value, _, err := k.GetStringValue(a.Name)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("autostart: read value: %w", err)
	}
	return value == a.Command, nil
}

func (a Autostart) Set(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, a.Key, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("autostart: open key: %w", err)
	}
	defer k.Close()

	if on {
		if err := k.SetStringValue(a.Name, a.Command); err != nil {
			return fmt.Errorf("autostart: write value: %w", err)
		}
		return nil
	}
	if err := k.DeleteValue(a.Name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("autostart: delete value: %w", err)
	}
	return nil
}

type Instance struct {
	handle windows.Handle
}

func AcquireInstance(name string) (*Instance, bool, error) {
	ptr, err := windows.UTF16PtrFromString(`Local\` + name)
	if err != nil {
		return nil, false, fmt.Errorf("instance name: %w", err)
	}
	h, err := windows.CreateMutex(nil, false, ptr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("create instance mutex: %w", err)
	}
	return &Instance{handle: h}, true, nil
}

func (i *Instance) Release() {
	_ = windows.CloseHandle(i.handle)
}

const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

func TaskbarIsLight() bool {
	return personalizeFlag("SystemUsesLightTheme")
}

func AppsAreLight() bool {
	return personalizeFlag("AppsUseLightTheme")
}

func personalizeFlag(name string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	v, _, err := k.GetIntegerValue(name)
	return err == nil && v != 0
}
