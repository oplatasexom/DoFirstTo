package catalog

import (
	"slices"
	"strings"
	"testing"

	"dofirst/internal/service/rules"
)

func TestAppsAreWellFormed(t *testing.T) {
	categories := Categories()
	ids := map[string]bool{}
	owners := map[string]string{}
	for _, app := range Apps() {
		if app.ID == "" || app.Name == "" || ids[app.ID] {
			t.Errorf("app %+v: empty or duplicate id/name", app)
		}
		ids[app.ID] = true
		if !slices.Contains(categories, app.Category) || app.Category == Other {
			t.Errorf("app %s: unknown category %q", app.ID, app.Category)
		}
		if len(app.Exes) == 0 {
			t.Errorf("app %s lists no programs", app.ID)
		}
		for _, exe := range app.Exes {
			rule, err := rules.Parse(exe)
			if err != nil || len(rule.Exes) != 1 || rule.Exes[0] != exe {
				t.Errorf("app %s: %q does not survive rule parsing (%v, %v)", app.ID, exe, rule.Exes, err)
			}
			if other, taken := owners[exe]; taken {
				t.Errorf("program %q belongs to both %s and %s", exe, other, app.ID)
			}
			owners[exe] = app.ID
		}
	}
}

func TestBrowsersAreKnownToRules(t *testing.T) {
	known := rules.DefaultOptions().Browsers
	for _, app := range Apps() {
		for _, exe := range app.Exes {
			if isBrowser := slices.Contains(known, exe); isBrowser != (app.Category == Browsers) {
				t.Errorf("%s (%s): category %q, in the rules browser list: %v", app.Name, exe, app.Category, isBrowser)
			}
		}
	}
}

func TestSitesAreWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, site := range Sites() {
		if site.ID == "" || site.Name == "" || ids[site.ID] {
			t.Errorf("site %+v: empty or duplicate id/name", site)
		}
		ids[site.ID] = true
		if !slices.Contains(Categories(), site.Category) {
			t.Errorf("site %s: unknown category %q", site.ID, site.Category)
		}
		if len(site.Titles) == 0 {
			t.Errorf("site %s has no title words", site.ID)
		}
		// Слова записываются в строку правила через запятую после черты
		rule, err := rules.Parse("chrome.exe | " + strings.Join(site.Titles, ", "))
		if err != nil || len(rule.Titles) != len(site.Titles) {
			t.Errorf("site %s: titles %q do not survive rule parsing (%q, %v)", site.ID, site.Titles, rule.Titles, err)
		}
	}
}

func TestSiteWordsMatchRealTabTitles(t *testing.T) {
	// Заголовки, которые браузеры показывают на этих сайтах
	tabs := map[string][]string{
		"claude":        {"Claude — Zen Browser", "Go channels - Claude - Google Chrome"},
		"deepseek":      {"DeepSeek - Into the Unknown - Google Chrome"},
		"gemini":        {"Google Gemini - Google Chrome"},
		"github":        {"GitHub - golang/go: The Go programming language — Zen Browser", "Issues · golang/go · GitHub"},
		"godocs":        {"fmt package - fmt - Go Packages - Google Chrome", "A Tour of Go — Mozilla Firefox"},
		"roadmap":       {"Go Roadmap - roadmap.sh — Zen Browser"},
		"pdf":           {"concurrency-in-go.pdf - Google Chrome"},
		"gdocs":         {"План - Google Документы - Google Chrome"},
		"stackoverflow": {"go - What is a rune? - Stack Overflow — Zen Browser"},
	}
	for _, site := range Sites() {
		rule, err := rules.Parse("zen.exe, chrome.exe, firefox.exe | " + strings.Join(site.Titles, ", "))
		if err != nil {
			t.Fatalf("site %s: %v", site.ID, err)
		}
		set := rules.NewSet([]rules.Rule{rule}, rules.DefaultOptions())
		for _, title := range tabs[site.ID] {
			if got := set.Check(rules.Window{Exe: "zen.exe", Title: title}); got != rules.Allow {
				t.Errorf("site %s: tab %q = %v, want allow", site.ID, title, got)
			}
		}
		if got := set.Check(rules.Window{Exe: "zen.exe", Title: "YouTube — Zen Browser"}); got != rules.BlockTitle {
			t.Errorf("site %s: an unrelated tab = %v, want block-title", site.ID, got)
		}
	}
}

func TestFind(t *testing.T) {
	tests := []struct {
		exe    string
		wantID string
	}{
		{"code.exe", "vscode"},
		{" Code.EXE ", "vscode"},
		{"pwsh.exe", "terminal"},
		{"яндекс музыка.exe", "yandexmusic"},
		{"ms-teams.exe", "teams"},
		{"qbittorrent.exe", ""},
		{"", ""},
	}
	for _, tt := range tests {
		app, ok := Find(tt.exe)
		if ok != (tt.wantID != "") || app.ID != tt.wantID {
			t.Errorf("Find(%q) = %q, %v; want %q", tt.exe, app.ID, ok, tt.wantID)
		}
	}
}
