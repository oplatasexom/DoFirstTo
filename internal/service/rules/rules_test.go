package rules

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"dofirst/internal/domain"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantExes   []string
		wantTitles []string
		wantErr    error
	}{
		{
			name:       "programs and titles",
			in:         "chrome.exe, browser.exe | Claude, roadmap.sh",
			wantExes:   []string{"chrome.exe", "browser.exe"},
			wantTitles: []string{"claude", "roadmap.sh"},
		},
		{name: "extension is added", in: "Code", wantExes: []string{"code.exe"}},
		{name: "dotted name gets extension", in: "notepad++, my.app", wantExes: []string{"notepad++.exe", "my.app.exe"}},
		{
			name:     "full path in quotes",
			in:       `"C:\Program Files\JetBrains\bin\GoLand64.EXE"`,
			wantExes: []string{"goland64.exe"},
		},
		{name: "empty title part", in: "chrome.exe |", wantExes: []string{"chrome.exe"}},
		{
			name:       "duplicates and blanks are dropped",
			in:         "chrome.exe, , CHROME.EXE | PDF, , pdf",
			wantExes:   []string{"chrome.exe"},
			wantTitles: []string{"pdf"},
		},
		{
			name:       "only first bar separates",
			in:         "chrome.exe | a|b",
			wantExes:   []string{"chrome.exe"},
			wantTitles: []string{"a|b"},
		},
		{name: "no programs", in: "| Claude", wantErr: ErrNoPrograms},
		{name: "empty", in: "   ", wantErr: ErrNoPrograms},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", tt.in, err, tt.wantErr)
			}
			if !slices.Equal(got.Exes, tt.wantExes) {
				t.Errorf("Exes = %q, want %q", got.Exes, tt.wantExes)
			}
			if !slices.Equal(got.Titles, tt.wantTitles) {
				t.Errorf("Titles = %q, want %q", got.Titles, tt.wantTitles)
			}
		})
	}
}

func TestRuleString(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "Chrome , browser.exe|Claude,  roadmap.sh", want: "chrome.exe, browser.exe | claude, roadmap.sh"},
		{in: "code", want: "code.exe"},
	}
	for _, tt := range tests {
		r, err := Parse(tt.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.in, err)
		}
		if got := r.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestCompile(t *testing.T) {
	groups := []domain.AppGroup{
		{ID: "web", Rule: "chrome.exe | Claude"},
		{ID: "ide", Rule: "code.exe"},
		{ID: "broken", Rule: "| only titles"},
	}

	t.Run("only active groups", func(t *testing.T) {
		got, err := Compile(groups, []string{"ide"})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if len(got) != 1 || !slices.Equal(got[0].Exes, []string{"code.exe"}) {
			t.Errorf("rules = %+v, want only code.exe", got)
		}
	})

	t.Run("bad groups are reported but do not stop the rest", func(t *testing.T) {
		got, err := Compile(groups, []string{"web", "broken", "ghost"})
		if len(got) != 1 || !slices.Equal(got[0].Exes, []string{"chrome.exe"}) {
			t.Errorf("rules = %+v, want only chrome.exe", got)
		}
		if !errors.Is(err, ErrNoPrograms) {
			t.Errorf("error = %v, want it to wrap ErrNoPrograms", err)
		}
		if err == nil || !strings.Contains(err.Error(), `group "ghost": not found`) {
			t.Errorf("error = %v, want it to mention the missing group", err)
		}
	})
}

func TestSetCheck(t *testing.T) {
	mustParse := func(s string) Rule {
		t.Helper()
		r, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		return r
	}
	opts := DefaultOptions()
	opts.Always = append(opts.Always, "Obsidian")
	set := NewSet([]Rule{
		mustParse("chrome.exe, firefox.exe | Claude, roadmap.sh"),
		mustParse("chrome.exe | .pdf"),
		mustParse("code.exe"),
		mustParse("msedge.exe | docs"),
		mustParse("msedge.exe"),
	}, opts)

	tests := []struct {
		name string
		win  Window
		want Verdict
	}{
		{name: "unrecognized program passes", win: Window{Exe: "", Title: "что-то"}, want: Allow},
		{name: "explorer always allowed", win: Window{Exe: "explorer.exe", Title: "Загрузки"}, want: Allow},
		{name: "task manager always allowed", win: Window{Exe: "Taskmgr.exe"}, want: Allow},
		{name: "user exception", win: Window{Exe: "Obsidian.exe", Title: "Vault"}, want: Allow},
		{name: "program without title filter", win: Window{Exe: "Code.exe", Title: "main.go"}, want: Allow},
		{name: "program not listed", win: Window{Exe: "Telegram.exe", Title: "Telegram"}, want: BlockApp},
		{name: "allowed tab", win: Window{Exe: "chrome.exe", Title: "Claude - Google Chrome"}, want: Allow},
		{name: "title match ignores case", win: Window{Exe: "chrome.exe", Title: "Backend Roadmap.SH - Google Chrome"}, want: Allow},
		{name: "titles from two rules add up", win: Window{Exe: "chrome.exe", Title: "book.pdf - Google Chrome"}, want: Allow},
		{name: "forbidden tab", win: Window{Exe: "chrome.exe", Title: "YouTube - Google Chrome"}, want: BlockTitle},
		{name: "new tab is neutral", win: Window{Exe: "chrome.exe", Title: "Новая вкладка - Google Chrome"}, want: Neutral},
		{name: "empty title is neutral", win: Window{Exe: "chrome.exe", Title: "  "}, want: Neutral},
		{name: "bare browser title is neutral", win: Window{Exe: "firefox.exe", Title: "Mozilla Firefox"}, want: Neutral},
		{name: "browser name inside a title is not neutral", win: Window{Exe: "firefox.exe", Title: "Видео — Mozilla Firefox"}, want: BlockTitle},
		{name: "rule without titles opens the whole program", win: Window{Exe: "msedge.exe", Title: "YouTube"}, want: Allow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := set.Check(tt.win); got != tt.want {
				t.Errorf("Check(%+v) = %v, want %v", tt.win, got, tt.want)
			}
		})
	}
}

func TestTitleWordsApplyOnlyToBrowsers(t *testing.T) {
	rule, err := Parse("chrome.exe, zen.exe, spotify.exe, code.exe | Claude, roadmap.sh")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	opts := DefaultOptions()
	opts.Browsers = append(opts.Browsers, "MyBrowser")
	custom, err := Parse("mybrowser.exe, notes.exe | docs")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	set := NewSet([]Rule{rule, custom}, opts)

	tests := []struct {
		name string
		win  Window
		want Verdict
	}{
		{name: "player next to a browser is allowed whole", win: Window{Exe: "Spotify.exe", Title: "Spotify Premium"}, want: Allow},
		{name: "editor next to a browser is allowed whole", win: Window{Exe: "Code.exe", Title: "main.go"}, want: Allow},
		{name: "program with an empty title is allowed", win: Window{Exe: "spotify.exe", Title: ""}, want: Allow},
		{name: "browser tab on the list", win: Window{Exe: "chrome.exe", Title: "Claude - Google Chrome"}, want: Allow},
		{name: "browser tab off the list", win: Window{Exe: "chrome.exe", Title: "YouTube - Google Chrome"}, want: BlockTitle},
		{name: "second browser follows the same words", win: Window{Exe: "zen.exe", Title: "Twitch — Zen Browser"}, want: BlockTitle},
		{name: "browser added by the user is filtered", win: Window{Exe: "mybrowser.exe", Title: "news"}, want: BlockTitle},
		{name: "its allowed page passes", win: Window{Exe: "mybrowser.exe", Title: "Go docs"}, want: Allow},
		{name: "program in the same rule is not filtered", win: Window{Exe: "notes.exe", Title: "shopping list"}, want: Allow},
		{name: "program outside every rule is blocked", win: Window{Exe: "telegram.exe", Title: "Claude"}, want: BlockApp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := set.Check(tt.win); got != tt.want {
				t.Errorf("Check(%+v) = %v, want %v", tt.win, got, tt.want)
			}
		})
	}

	t.Run("without a browser list nothing is filtered by title", func(t *testing.T) {
		bare := NewSet([]Rule{rule}, Options{})
		if got := bare.Check(Window{Exe: "chrome.exe", Title: "YouTube"}); got != Allow {
			t.Errorf("Check = %v, want %v", got, Allow)
		}
	})
}

func TestEmptySetBlocksEverythingButSystem(t *testing.T) {
	set := NewSet(nil, DefaultOptions())
	if got := set.Check(Window{Exe: "chrome.exe", Title: "Claude"}); got != BlockApp {
		t.Errorf("chrome = %v, want %v", got, BlockApp)
	}
	if got := set.Check(Window{Exe: "explorer.exe"}); got != Allow {
		t.Errorf("explorer = %v, want %v", got, Allow)
	}
}
