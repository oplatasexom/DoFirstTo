//go:build windows

package win

import (
	"testing"
	"unsafe"
)

func TestParseHotkey(t *testing.T) {
	tests := []struct {
		in       string
		wantMods uint32
		wantKey  uint32
		wantErr  bool
	}{
		{in: "Ctrl+Alt+D", wantMods: modControl | modAlt, wantKey: 'D'},
		{in: " ctrl + shift + 7 ", wantMods: modControl | modShift, wantKey: '7'},
		{in: "Win+F12", wantMods: modWin, wantKey: vkF1 + 11},
		{in: "Control+Space", wantMods: modControl, wantKey: vkSpace},
		{in: "Alt+F24", wantMods: modAlt, wantKey: vkF1 + 23},
		{in: "D", wantErr: true},
		{in: "Ctrl+Alt", wantErr: true},
		{in: "Ctrl+A+B", wantErr: true},
		{in: "Ctrl++D", wantErr: true},
		{in: "Ctrl+F25", wantErr: true},
		{in: "Ctrl+Enter", wantErr: true},
		{in: "Ctrl+Ж", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			mods, key, err := ParseHotkey(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseHotkey(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && (mods != tt.wantMods || key != tt.wantKey) {
				t.Errorf("ParseHotkey(%q) = %#x, %#x; want %#x, %#x", tt.in, mods, key, tt.wantMods, tt.wantKey)
			}
		})
	}
}

func TestStructLayouts(t *testing.T) {
	sizes := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"NOTIFYICONDATAW", unsafe.Sizeof(notifyIconData{}), 976},
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), 80},
		{"MSG", unsafe.Sizeof(winMsg{}), 48},
	}
	for _, s := range sizes {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}
}

func TestWorkArea(t *testing.T) {
	work, full, err := WorkArea()
	if err != nil {
		t.Skipf("no monitor in this session: %v", err)
	}
	if work.Width() <= 0 || work.Height() <= 0 || work.Width() > full.Width() || work.Height() > full.Height() {
		t.Errorf("work = %+v, full = %+v: work area must be a non-empty part of the monitor", work, full)
	}
	if Scale(100) < 100 {
		t.Errorf("Scale(100) = %d, want at least 100", Scale(100))
	}
	if SmallIconSize() < 16 {
		t.Errorf("SmallIconSize() = %d, want at least 16", SmallIconSize())
	}
}
