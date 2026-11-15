package domain

import (
	"testing"
	"time"
)

func TestTaskListForDay(t *testing.T) {
	list := TaskList{StrictOrder: true, Tasks: []Task{
		{ID: "daily"},
		{ID: "gym", Days: []time.Weekday{time.Monday, time.Wednesday, time.Friday}},
		{ID: "review", Days: []time.Weekday{time.Sunday}},
	}}
	tests := []struct {
		day  time.Weekday
		want []string
	}{
		{day: time.Monday, want: []string{"daily", "gym"}},
		{day: time.Tuesday, want: []string{"daily"}},
		{day: time.Sunday, want: []string{"daily", "review"}},
	}
	for _, tt := range tests {
		t.Run(tt.day.String(), func(t *testing.T) {
			got := list.ForDay(tt.day)
			if !got.StrictOrder {
				t.Error("ForDay lost the strict order flag")
			}
			if len(got.Tasks) != len(tt.want) {
				t.Fatalf("tasks = %v, want %v", got.Tasks, tt.want)
			}
			for i, id := range tt.want {
				if got.Tasks[i].ID != id {
					t.Errorf("task %d = %q, want %q", i, got.Tasks[i].ID, id)
				}
			}
		})
	}
	if len(list.Tasks) != 3 {
		t.Error("ForDay changed the original list")
	}
}

func TestBuiltinThemes(t *testing.T) {
	seen := map[string]bool{}
	dark, light := 0, 0
	for _, th := range BuiltinThemes() {
		if err := th.Validate(); err != nil {
			t.Errorf("theme %q: %v", th.ID, err)
		}
		if seen[th.ID] {
			t.Errorf("theme id %q is used twice", th.ID)
		}
		seen[th.ID] = true
		if th.Light() {
			light++
		} else {
			dark++
		}
	}
	if !seen[DefaultThemeID] {
		t.Errorf("default theme %q is not among the built-in ones", DefaultThemeID)
	}
	if dark != 5 || light != 5 {
		t.Errorf("dark/light themes = %d/%d, want 5/5", dark, light)
	}
}

func TestActiveTheme(t *testing.T) {
	own := Theme{ID: "th1", Name: "Моя", Background: "#fefefe", Accent: "#0055ff", Text: "#111111"}
	broken := Theme{ID: "th2", Background: "white", Accent: "#0055ff", Text: "#111111"}
	tests := []struct {
		name   string
		theme  string
		themes []Theme
		want   string
	}{
		{name: "nothing chosen", want: DefaultThemeID},
		{name: "built-in", theme: "mint", want: "mint"},
		{name: "own", theme: "th1", themes: []Theme{own}, want: "th1"},
		{name: "deleted own theme falls back", theme: "th1", want: DefaultThemeID},
		{name: "broken own theme falls back", theme: "th2", themes: []Theme{broken}, want: DefaultThemeID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			s.Theme, s.Themes = tt.theme, tt.themes
			if got := s.ActiveTheme().ID; got != tt.want {
				t.Errorf("ActiveTheme = %q, want %q", got, tt.want)
			}
		})
	}

	if r, g, b := own.BackgroundRGB(); r != 0xfe || g != 0xfe || b != 0xfe {
		t.Errorf("BackgroundRGB = %d %d %d", r, g, b)
	}
	if !own.Light() {
		t.Error("a theme with a white background must be light")
	}
	for _, bad := range []string{"", "#fff", "#gggggg", "ffffff0", "#+fffff"} {
		if _, _, _, ok := parseColor(bad); ok {
			t.Errorf("parseColor(%q) accepted a bad color", bad)
		}
	}
}
