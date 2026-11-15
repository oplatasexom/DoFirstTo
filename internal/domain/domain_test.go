package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in      string
		want    TimeOfDay
		wantErr bool
	}{
		{in: "07:30", want: 7*60 + 30},
		{in: " 23:00 ", want: 23 * 60},
		{in: "0:05", want: 5},
		{in: "24:00", wantErr: true},
		{in: "12:60", wantErr: true},
		{in: "-1:00", wantErr: true},
		{in: "1230", wantErr: true},
		{in: "ab:cd", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTimeOfDay(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTimeOfDay(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseTimeOfDay(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTimeOfDayJSON(t *testing.T) {
	release := TimeOfDay(13 * 60)
	in := Schedule{Bedtime: 23*60 + 15, WakeTime: 7 * 60, AutoRelease: &release}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"bedtime":"23:15"`, `"wakeTime":"07:00"`, `"autoRelease":"13:00"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON %s does not contain %s", raw, want)
		}
	}

	var out Schedule
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Bedtime != in.Bedtime || out.WakeTime != in.WakeTime || out.AutoRelease == nil || *out.AutoRelease != release {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

func TestTimeOfDayOn(t *testing.T) {
	loc := time.FixedZone("T", 3*3600)
	day := time.Date(2026, 11, 11, 18, 45, 12, 0, loc)
	got := TimeOfDay(7*60 + 30).On(day)
	want := time.Date(2026, 11, 11, 7, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("On = %v, want %v", got, want)
	}
}

func TestSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Settings)
		wantErr string // подстрока ошибки; пусто — ошибки быть не должно
	}{
		{name: "defaults are valid", mutate: func(*Settings) {}},
		{
			name:    "bedtime out of range",
			mutate:  func(s *Settings) { s.Schedule.Bedtime = 24 * 60 },
			wantErr: "bedtime",
		},
		{
			name:    "duplicate group",
			mutate:  func(s *Settings) { s.Groups = append(s.Groups, s.Groups[0]) },
			wantErr: "duplicate id",
		},
		{
			name:    "duplicate task",
			mutate:  func(s *Settings) { s.Tasks.Tasks = append(s.Tasks.Tasks, s.Tasks.Tasks[0]) },
			wantErr: `duplicate id "teeth"`,
		},
		{
			name:    "empty title",
			mutate:  func(s *Settings) { s.Tasks.Tasks[0].Title = "  " },
			wantErr: "empty title",
		},
		{
			name:    "timer without minutes",
			mutate:  func(s *Settings) { s.Tasks.Tasks[1].MinMinutes = 0 },
			wantErr: "timer needs minutes",
		},
		{
			name:    "unknown kind",
			mutate:  func(s *Settings) { s.Tasks.Tasks[0].Kind = "magic" },
			wantErr: "unknown kind",
		},
		{
			name:    "unknown group reference",
			mutate:  func(s *Settings) { s.Tasks.Tasks[0].Unlocks = []string{"games"} },
			wantErr: `unknown group "games"`,
		},
		{
			name:    "negative tab countdown",
			mutate:  func(s *Settings) { s.TabCountdownSec = -1 },
			wantErr: "tab countdown",
		},
		{
			name:    "unknown night group",
			mutate:  func(s *Settings) { s.Schedule.NightGroups = []string{"games"} },
			wantErr: `unknown night group "games"`,
		},
		{
			name:   "task on chosen weekdays",
			mutate: func(s *Settings) { s.Tasks.Tasks[0].Days = []time.Weekday{time.Monday, time.Sunday} },
		},
		{
			name:    "task weekday out of range",
			mutate:  func(s *Settings) { s.Tasks.Tasks[0].Days = []time.Weekday{7} },
			wantErr: "task #1: weekday 7",
		},
		{
			name: "own theme",
			mutate: func(s *Settings) {
				s.Themes = []Theme{{ID: "th1", Name: "Моя", Background: "#101010", Accent: "#ff8800", Text: "#eeeeee"}}
				s.Theme = "th1"
			},
		},
		{
			name: "own theme with a bad color",
			mutate: func(s *Settings) {
				s.Themes = []Theme{{ID: "th1", Background: "#101010", Accent: "orange", Text: "#eeeeee"}}
			},
			wantErr: `accent color "orange"`,
		},
		{
			name: "own theme may not take a built-in id",
			mutate: func(s *Settings) {
				s.Themes = []Theme{{ID: "mint", Background: "#101010", Accent: "#ff8800", Text: "#eeeeee"}}
			},
			wantErr: `duplicate id "mint"`,
		},
		{
			name: "own theme with a long name",
			mutate: func(s *Settings) {
				s.Themes = []Theme{{ID: "th1", Name: strings.Repeat("я", 41), Background: "#101010", Accent: "#ff8800", Text: "#eeeeee"}}
			},
			wantErr: "name is longer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := DefaultSettings()
			tt.mutate(&s)
			err := s.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}
