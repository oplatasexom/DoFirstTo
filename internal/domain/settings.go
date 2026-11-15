package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type TaskKind string

const (
	KindCheck TaskKind = "check"
	KindTimer TaskKind = "timer"
)

type AppGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Rule string `json:"rule"`
}

type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Kind        TaskKind `json:"kind"`
	MinMinutes int `json:"minMinutes,omitempty"`
	RequireNote bool `json:"requireNote,omitempty"`
	Journal bool `json:"journal,omitempty"`
	Needs []string `json:"needs,omitempty"`
	Unlocks []string `json:"unlocks,omitempty"`
	Launch []string `json:"launch,omitempty"`
	Days []time.Weekday `json:"days,omitempty"`
}

func (t Task) On(day time.Weekday) bool {
	return len(t.Days) == 0 || slices.Contains(t.Days, day)
}

type TaskList struct {
	Tasks []Task `json:"tasks"`
	StrictOrder bool `json:"strictOrder"`
}

func (l TaskList) ForDay(day time.Weekday) TaskList {
	out := TaskList{StrictOrder: l.StrictOrder, Tasks: make([]Task, 0, len(l.Tasks))}
	for _, t := range l.Tasks {
		if t.On(day) {
			out.Tasks = append(out.Tasks, t)
		}
	}
	return out
}

type Schedule struct {
	Enabled bool `json:"enabled"`
	Bedtime TimeOfDay `json:"bedtime"`
	WakeTime TimeOfDay `json:"wakeTime"`
	Days []time.Weekday `json:"days"`
	AutoRelease *TimeOfDay `json:"autoRelease,omitempty"`
	NightScreen bool `json:"nightScreen"`
	NightGroups []string `json:"nightGroups,omitempty"`
}

type Settings struct {
	Language string     `json:"language"`
	Schedule Schedule   `json:"schedule"`
	Tasks    TaskList   `json:"tasks"`
	Groups   []AppGroup `json:"groups"`
	AlwaysAllowed []string `json:"alwaysAllowed,omitempty"`
	TabCountdownSec int    `json:"tabCountdownSec"`
	Autostart       bool   `json:"autostart"`
	Hotkey          string `json:"hotkey"`
	Theme string `json:"theme,omitempty"`
	Themes []Theme `json:"themes,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		Language: "ru",
		Schedule: Schedule{
			Enabled:  true,
			Bedtime:  23 * 60,
			WakeTime: 7 * 60,
			Days: []time.Weekday{
				time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
				time.Friday, time.Saturday, time.Sunday,
			},
		},
		Groups: []AppGroup{
			{ID: "study-web", Name: "Учёба в браузере", Rule: "chrome.exe, msedge.exe, firefox.exe, zen.exe, browser.exe | Claude, roadmap.sh, .pdf"},
			{ID: "ide", Name: "Редактор кода", Rule: "code.exe, goland64.exe"},
		},
		Tasks: TaskList{
			StrictOrder: true,
			Tasks: []Task{
				{ID: "teeth", Title: "Почистить зубы", Kind: KindCheck},
				{
					ID: "study", Title: "Roadmap", Kind: KindTimer, MinMinutes: 60,
					RequireNote: true, Journal: true,
					Needs:  []string{"study-web", "ide"},
					Launch: []string{"https://roadmap.sh"},
				},
			},
		},
		TabCountdownSec: 5,
		Autostart:       true,
		Hotkey:          "Ctrl+Alt+D",
	}
}

func (s Settings) Validate() error {
	var errs []error

	if !s.Schedule.Bedtime.Valid() {
		errs = append(errs, errors.New("schedule: bedtime out of range"))
	}
	if !s.Schedule.WakeTime.Valid() {
		errs = append(errs, errors.New("schedule: wake time out of range"))
	}
	if r := s.Schedule.AutoRelease; r != nil && !r.Valid() {
		errs = append(errs, errors.New("schedule: auto release out of range"))
	}
	for _, d := range s.Schedule.Days {
		if d < time.Sunday || d > time.Saturday {
			errs = append(errs, fmt.Errorf("schedule: weekday %d out of range", int(d)))
		}
	}
	if s.TabCountdownSec < 0 {
		errs = append(errs, errors.New("tab countdown must not be negative"))
	}

	groups := make(map[string]struct{}, len(s.Groups))
	for i, g := range s.Groups {
		switch _, dup := groups[g.ID]; {
		case strings.TrimSpace(g.ID) == "":
			errs = append(errs, fmt.Errorf("group #%d: empty id", i+1))
		case dup:
			errs = append(errs, fmt.Errorf("group %q: duplicate id", g.ID))
		default:
			groups[g.ID] = struct{}{}
		}
	}

	for _, id := range s.Schedule.NightGroups {
		if _, ok := groups[id]; !ok {
			errs = append(errs, fmt.Errorf("schedule: unknown night group %q", id))
		}
	}

	tasks := make(map[string]struct{}, len(s.Tasks.Tasks))
	for i, t := range s.Tasks.Tasks {
		label := fmt.Sprintf("task #%d", i+1)
		switch _, dup := tasks[t.ID]; {
		case strings.TrimSpace(t.ID) == "":
			errs = append(errs, fmt.Errorf("%s: empty id", label))
		case dup:
			errs = append(errs, fmt.Errorf("%s: duplicate id %q", label, t.ID))
		default:
			tasks[t.ID] = struct{}{}
		}
		if strings.TrimSpace(t.Title) == "" {
			errs = append(errs, fmt.Errorf("%s: empty title", label))
		}
		switch t.Kind {
		case KindCheck:
		case KindTimer:
			if t.MinMinutes <= 0 {
				errs = append(errs, fmt.Errorf("%s: timer needs minutes > 0", label))
			}
		default:
			errs = append(errs, fmt.Errorf("%s: unknown kind %q", label, t.Kind))
		}
		for _, id := range append(append([]string{}, t.Needs...), t.Unlocks...) {
			if _, ok := groups[id]; !ok {
				errs = append(errs, fmt.Errorf("%s: unknown group %q", label, id))
			}
		}
		for _, d := range t.Days {
			if d < time.Sunday || d > time.Saturday {
				errs = append(errs, fmt.Errorf("%s: weekday %d out of range", label, int(d)))
			}
		}
	}

	themes := make(map[string]struct{}, len(s.Themes))
	for i, th := range s.Themes {
		label := fmt.Sprintf("theme #%d", i+1)
		_, dup := themes[th.ID]
		_, builtin := builtinTheme(th.ID)
		switch {
		case strings.TrimSpace(th.ID) == "":
			errs = append(errs, fmt.Errorf("%s: empty id", label))
		case dup || builtin:
			errs = append(errs, fmt.Errorf("%s: duplicate id %q", label, th.ID))
		default:
			themes[th.ID] = struct{}{}
		}
		if err := th.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label, err))
		}
	}

	return errors.Join(errs...)
}
