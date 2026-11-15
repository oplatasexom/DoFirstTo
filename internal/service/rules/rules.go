package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"dofirst/internal/domain"
)

var ErrNoPrograms = errors.New("rule lists no programs")

type Rule struct {
	Exes []string
	Titles []string
}

func Parse(s string) (Rule, error) {
	left, right, _ := strings.Cut(s, "|")

	var r Rule
	for _, part := range strings.Split(left, ",") {
		if exe := normalizeExe(part); exe != "" && !slices.Contains(r.Exes, exe) {
			r.Exes = append(r.Exes, exe)
		}
	}
	if len(r.Exes) == 0 {
		return Rule{}, ErrNoPrograms
	}
	for _, part := range strings.Split(right, ",") {
		if title := strings.ToLower(strings.TrimSpace(part)); title != "" && !slices.Contains(r.Titles, title) {
			r.Titles = append(r.Titles, title)
		}
	}
	return r, nil
}

func (r Rule) String() string {
	s := strings.Join(r.Exes, ", ")
	if len(r.Titles) > 0 {
		s += " | " + strings.Join(r.Titles, ", ")
	}
	return s
}

func normalizeExe(s string) string {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), `"'`))
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasSuffix(s, ".exe") {
		s += ".exe"
	}
	return s
}

func Compile(groups []domain.AppGroup, active []string) ([]Rule, error) {
	byID := make(map[string]domain.AppGroup, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}

	var (
		out  []Rule
		errs []error
	)
	for _, id := range active {
		g, ok := byID[id]
		if !ok {
			errs = append(errs, fmt.Errorf("group %q: not found", id))
			continue
		}
		r, err := Parse(g.Rule)
		if err != nil {
			errs = append(errs, fmt.Errorf("group %q: %w", id, err))
			continue
		}
		out = append(out, r)
	}
	return out, errors.Join(errs...)
}

type Window struct {
	Exe string
	Title string
}

type Verdict int

const (
	Allow Verdict = iota
	Neutral
	BlockTitle
	BlockApp
)

func (v Verdict) String() string {
	switch v {
	case Allow:
		return "allow"
	case Neutral:
		return "neutral"
	case BlockTitle:
		return "block-title"
	case BlockApp:
		return "block-app"
	default:
		return "unknown"
	}
}

type Options struct {
	Always []string
	NeutralParts []string
	NeutralExact []string
	Browsers []string
}

func DefaultOptions() Options {
	return Options{
		Always: []string{
			"explorer.exe", "taskmgr.exe",
			"startmenuexperiencehost.exe", "searchhost.exe", "searchapp.exe",
			"shellexperiencehost.exe", "textinputhost.exe", "sihost.exe",
			"lockapp.exe", "logonui.exe", "consent.exe", "dwm.exe", "ctfmon.exe",
		},
		NeutralParts: []string{"new tab", "новая вкладка", "about:blank"},
		NeutralExact: []string{"mozilla firefox", "zen browser", "google chrome", "microsoft edge", "яндекс браузер"},
		Browsers: []string{
			"chrome.exe", "msedge.exe", "firefox.exe", "zen.exe", "browser.exe",
			"opera.exe", "brave.exe", "vivaldi.exe", "arc.exe", "chromium.exe",
			"librewolf.exe", "waterfox.exe", "floorp.exe", "thorium.exe", "iexplore.exe",
		},
	}
}

type Set struct {
	always       map[string]struct{}
	programs     map[string]*program
	neutralParts []string
	neutralExact []string
}

type program struct {
	anyTitle bool
	titles   []string
}

func NewSet(rules []Rule, opts Options) *Set {
	s := &Set{
		always:   make(map[string]struct{}, len(opts.Always)),
		programs: make(map[string]*program),
	}
	browsers := make(map[string]struct{}, len(opts.Browsers))
	for _, exe := range opts.Browsers {
		if exe = normalizeExe(exe); exe != "" {
			browsers[exe] = struct{}{}
		}
	}
	for _, exe := range opts.Always {
		if exe = normalizeExe(exe); exe != "" {
			s.always[exe] = struct{}{}
		}
	}
	for _, part := range opts.NeutralParts {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			s.neutralParts = append(s.neutralParts, part)
		}
	}
	for _, title := range opts.NeutralExact {
		if title = strings.ToLower(strings.TrimSpace(title)); title != "" {
			s.neutralExact = append(s.neutralExact, title)
		}
	}
	for _, r := range rules {
		for _, exe := range r.Exes {
			p, ok := s.programs[exe]
			if !ok {
				p = &program{}
				s.programs[exe] = p
			}
			if _, browser := browsers[exe]; len(r.Titles) == 0 || !browser {
				p.anyTitle = true
			}
			p.titles = append(p.titles, r.Titles...)
		}
	}
	return s
}

func (s *Set) Check(w Window) Verdict {
	exe := strings.ToLower(strings.TrimSpace(w.Exe))
	if exe == "" {
		return Allow
	}
	if _, ok := s.always[exe]; ok {
		return Allow
	}
	p, ok := s.programs[exe]
	if !ok {
		return BlockApp
	}
	if p.anyTitle {
		return Allow
	}

	title := strings.ToLower(strings.TrimSpace(w.Title))
	if title == "" {
		return Neutral
	}
	for _, part := range p.titles {
		if strings.Contains(title, part) {
			return Allow
		}
	}
	if slices.Contains(s.neutralExact, title) {
		return Neutral
	}
	for _, part := range s.neutralParts {
		if strings.Contains(title, part) {
			return Neutral
		}
	}
	return BlockTitle
}
