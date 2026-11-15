package ui

import "testing"

func TestPopupWatch(t *testing.T) {
	const self, other, taskbar = 10, 20, 30
	tests := []struct {
		name   string
		probes []popupProbe
		want   []bool
	}{
		{
			name: "click outside closes even if the popup never became active",
			probes: []popupProbe{
				{Foreground: taskbar, Self: self},
				{ButtonDown: true, Foreground: taskbar, Self: self},
			},
			want: []bool{false, true},
		},
		{
			name: "click inside keeps it open",
			probes: []popupProbe{
				{Foreground: self, Self: self},
				{ButtonDown: true, CursorInside: true, Foreground: self, Self: self},
				{Foreground: self, Self: self},
			},
			want: []bool{false, false, false},
		},
		{
			name: "button still held from the opening click is ignored",
			probes: []popupProbe{
				{ButtonDown: true, Foreground: taskbar, Self: self},
				{ButtonDown: true, Foreground: taskbar, Self: self},
				{Foreground: taskbar, Self: self},
				{ButtonDown: true, Foreground: taskbar, Self: self},
			},
			want: []bool{false, false, false, true},
		},
		{
			name: "switching to another window closes an active popup",
			probes: []popupProbe{
				{Foreground: self, Self: self},
				{Foreground: other, Self: self},
			},
			want: []bool{false, true},
		},
		{
			name: "foreign foreground does not close a popup that was never active",
			probes: []popupProbe{
				{Foreground: taskbar, Self: self},
				{Foreground: other, Self: self},
			},
			want: []bool{false, false},
		},
		{
			name: "no foreground window during a switch is not a reason to close",
			probes: []popupProbe{
				{Foreground: self, Self: self},
				{Foreground: 0, Self: self},
				{Foreground: self, Self: self},
			},
			want: []bool{false, false, false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var watch popupWatch
			for i, probe := range tt.probes {
				if got := watch.shouldClose(probe); got != tt.want[i] {
					t.Fatalf("probe %d: shouldClose = %v, want %v", i, got, tt.want[i])
				}
			}
		})
	}
}
