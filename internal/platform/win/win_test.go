//go:build windows

package win

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestKeyInputLayout(t *testing.T) {
	if got := unsafe.Sizeof(keyInput{}); got != 40 {
		t.Fatalf("sizeof(keyInput) = %d, want 40", got)
	}
	var k keyInput
	if off := unsafe.Offsetof(k.vk); off != 8 {
		t.Errorf("offset of vk = %d, want 8", off)
	}
	if off := unsafe.Offsetof(k.extra); off != 24 {
		t.Errorf("offset of extra = %d, want 24", off)
	}
}

func TestExeName(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	want := strings.ToLower(filepath.Base(self))
	if got := exeName(windows.GetCurrentProcessId()); got != want {
		t.Errorf("exeName(self) = %q, want %q", got, want)
	}
	if got := exeName(0xFFFFFFF0); got != "" {
		t.Errorf("exeName(bogus pid) = %q, want empty", got)
	}
}

func TestInspector(t *testing.T) {
	i := NewInspector()

	w, err := i.Foreground()
	if err != nil {
		t.Fatalf("Foreground: %v", err)
	}
	if w.ID == 0 && (w.Exe != "" || w.Title != "") {
		t.Errorf("window without ID has data: %+v", w)
	}

	for _, p := range i.OpenPrograms() {
		if p.Exe == "" || strings.TrimSpace(p.Title) == "" || p.Path == "" {
			t.Errorf("OpenPrograms returned an unusable entry: %+v", p)
		}
		if p.Exe != strings.ToLower(filepath.Base(p.Path)) {
			t.Errorf("exe %q does not match the path %q", p.Exe, p.Path)
		}
	}
}

func TestInstalledPrograms(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("depends on the programs installed on this machine")
	}
	list := InstalledPrograms()
	if len(list) == 0 {
		t.Skip("no shortcuts in the Start menu of this session")
	}
	seen := map[string]bool{}
	for _, p := range list {
		if p.Name == "" || !strings.HasSuffix(p.Exe, ".exe") || p.Exe != strings.ToLower(p.Exe) {
			t.Errorf("unusable entry: %+v", p)
		}
		if seen[p.Exe] {
			t.Errorf("program %s is listed twice", p.Exe)
		}
		seen[p.Exe] = true
		if os.Getenv("DOFIRST_LIST_APPS") != "" {
			note := ""
			if _, err := IconPNG(p.IconPath); err != nil {
				note = "  [no icon: " + err.Error() + "]"
			}
			t.Logf("%-28s %s%s", p.Exe, p.Name, note)
		}
	}
	pick := list[0]
	for _, p := range list {
		if p.Exe == "code.exe" || p.Exe == "telegram.exe" {
			pick = p
		}
	}
	data, err := IconPNG(pick.IconPath)
	if err != nil {
		t.Fatalf("IconPNG(%s): %v", pick.IconPath, err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("icon is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() < 16 || b.Dx() != b.Dy() {
		t.Errorf("icon size = %dx%d, want a square of at least 16", b.Dx(), b.Dy())
	}
	if out := os.Getenv("DOFIRST_ICON_OUT"); out != "" {
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	var wg sync.WaitGroup
	failed := make([]error, len(list))
	for i, p := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, failed[i] = IconPNG(p.IconPath)
		}()
	}
	wg.Wait()
	for i, err := range failed {
		if err != nil {
			t.Errorf("parallel IconPNG of %s: %v", list[i].Exe, err)
		}
	}

	if _, err := IconPNG(`C:\no\such\file.exe`); err == nil {
		t.Log("a missing file still gets a generic icon from the shell")
	}
}

func TestControllerRefusesInactiveWindow(t *testing.T) {
	sent, err := Controller{}.CloseTab(^uintptr(0))
	if sent || err != nil {
		t.Errorf("CloseTab(inactive) = %v, %v; want false, nil", sent, err)
	}
	if err := (Controller{}).Minimize(0); err == nil {
		t.Error("Minimize(0) = nil, want error")
	}
}

func TestAutostart(t *testing.T) {
	key := fmt.Sprintf(`Software\DoFirstTo-test-%d\Run`, os.Getpid())
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, filepath.Dir(key))
	})
	a := Autostart{Key: key, Name: "DoFirstTo", Command: `"C:\Apps\dofirst.exe" --autostart`}

	steps := []struct {
		name string
		do   func() error
		want bool
	}{
		{name: "missing key", do: func() error { return nil }, want: false},
		{name: "enable", do: func() error { return a.Set(true) }, want: true},
		{name: "enable twice", do: func() error { return a.Set(true) }, want: true},
		{name: "disable", do: func() error { return a.Set(false) }, want: false},
		{name: "disable twice", do: func() error { return a.Set(false) }, want: false},
	}
	for _, st := range steps {
		if err := st.do(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		got, err := a.Enabled()
		if err != nil {
			t.Fatalf("%s: Enabled: %v", st.name, err)
		}
		if got != st.want {
			t.Errorf("%s: Enabled = %v, want %v", st.name, got, st.want)
		}
	}

	if err := a.Set(true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	moved := a
	moved.Command = `"D:\New\dofirst.exe" --autostart`
	if on, err := moved.Enabled(); err != nil || on {
		t.Errorf("Enabled with another command = %v, %v; want false, nil", on, err)
	}
}

func TestAcquireInstance(t *testing.T) {
	name := fmt.Sprintf("DoFirstTo-test-%d", os.Getpid())

	first, ok, err := AcquireInstance(name)
	if err != nil || !ok {
		t.Fatalf("first AcquireInstance = %v, %v; want acquired", ok, err)
	}
	if _, ok, err := AcquireInstance(name); err != nil || ok {
		t.Errorf("second AcquireInstance = %v, %v; want refused", ok, err)
	}
	first.Release()

	again, ok, err := AcquireInstance(name)
	if err != nil || !ok {
		t.Fatalf("AcquireInstance after release = %v, %v; want acquired", ok, err)
	}
	again.Release()
}

func TestThemeFlagsDoNotPanic(t *testing.T) {
	_ = TaskbarIsLight()
	_ = AppsAreLight()
}
