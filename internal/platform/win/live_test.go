//go:build windows

package win

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"dofirst/internal/service/guard"
	"dofirst/internal/service/rules"
)

func TestLiveBlocking(t *testing.T) {
	if os.Getenv("DOFIRST_LIVE") == "" {
		t.Skip("set DOFIRST_LIVE=1 to open real windows")
	}
	insp := NewInspector()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	tests := []struct {
		name   string
		launch string
		exe    string
	}{
		{name: "classic program", launch: "charmap.exe", exe: "charmap.exe"},
		{name: "store app inside a frame host", launch: "calc.exe", exe: "calculatorapp.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := windowsOf(insp, tt.exe)
			if err := exec.Command(tt.launch).Start(); err != nil {
				t.Skipf("cannot start %s: %v", tt.launch, err)
			}
			hwnd := waitNewWindow(insp, tt.exe, before)
			if hwnd == 0 {
				var seen []string
				for _, p := range insp.OpenPrograms() {
					seen = append(seen, p.Exe)
				}
				t.Fatalf("no window of %s appeared; open programs: %v", tt.exe, seen)
			}
			t.Cleanup(func() {
				const wmCloseMessage = 0x0010
				procPostMessageW.Call(hwnd, wmCloseMessage, 0, 0)
			})

			rec := &liveRecorder{}
			g := guard.New(insp, Controller{}, rec, guard.DefaultConfig(5*time.Second), log)

			allowed, err := rules.Parse(tt.exe)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			g.SetRules(rules.NewSet([]rules.Rule{allowed}, rules.DefaultOptions()))
			focusWindow(t, insp, hwnd)
			g.Step(time.Now())
			time.Sleep(300 * time.Millisecond)
			if isMinimized(hwnd) || len(rec.blocked) != 0 {
				t.Fatalf("allowed program was touched: minimized=%v, blocked=%v", isMinimized(hwnd), rec.blocked)
			}
			if !slices.Contains(rec.allowed, tt.exe) {
				t.Errorf("allowed program was not reported: %v", rec.allowed)
			}

			g.SetRules(rules.NewSet(nil, rules.DefaultOptions()))
			focusWindow(t, insp, hwnd)
			g.Step(time.Now())
			deadline := time.Now().Add(3 * time.Second)
			for !isMinimized(hwnd) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if !isMinimized(hwnd) {
				t.Errorf("forbidden program %s is still on screen", tt.exe)
			}
			if !slices.Equal(rec.blocked, []string{tt.exe}) {
				t.Errorf("blocked = %v, want [%s]", rec.blocked, tt.exe)
			}
		})
	}
}

type liveRecorder struct {
	blocked []string
	allowed []string
}

func (r *liveRecorder) Blocked(w guard.Window, _ guard.Reason) { r.blocked = append(r.blocked, w.Exe) }
func (r *liveRecorder) Allowed(w guard.Window)                 { r.allowed = append(r.allowed, w.Exe) }
func (r *liveRecorder) TabWarning(guard.Window, int)           {}

var procIsIconic = user32.NewProc("IsIconic")

func isMinimized(hwnd uintptr) bool {
	iconic, _, _ := procIsIconic.Call(hwnd)
	return iconic != 0
}

func windowsOf(i *Inspector, exe string) []uintptr {
	i.mu.Lock()
	i.list = i.list[:0]
	procEnumWindows.Call(i.listCB, 0)
	handles := slices.Clone(i.list)
	i.mu.Unlock()

	var out []uintptr
	for _, hwnd := range handles {
		if i.describe(hwnd).Exe == exe {
			out = append(out, hwnd)
		}
	}
	return out
}

func waitNewWindow(i *Inspector, exe string, before []uintptr) uintptr {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, hwnd := range windowsOf(i, exe) {
			if !slices.Contains(before, hwnd) {
				return hwnd
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

func focusWindow(t *testing.T, i *Inspector, hwnd uintptr) {
	t.Helper()
	const swRestore = 9
	procShowWindow.Call(hwnd, swRestore)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		forceForeground(hwnd)
		time.Sleep(150 * time.Millisecond)
		if w, _ := i.Foreground(); w.ID == hwnd {
			return
		}
	}
	w, _ := i.Foreground()
	t.Skipf("Windows did not let the test bring the window forward (foreground: %q)", w.Exe)
}
