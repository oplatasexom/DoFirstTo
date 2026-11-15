package domain

import (
	"errors"
	"fmt"
	"strconv"
	"unicode/utf8"
)

const DefaultThemeID = "espresso"

const maxThemeName = 40

type Theme struct {
	ID string `json:"id"`
	Name string `json:"name,omitempty"`
	Background string `json:"background"`
	Accent string `json:"accent"`
	Text string `json:"text"`
}

func BuiltinThemes() []Theme {
	return []Theme{
		{ID: DefaultThemeID, Background: "#1a1209", Accent: "#598175", Text: "#f3ecdd"},
		{ID: "forest", Background: "#080d06", Accent: "#152c18", Text: "#8aa169"},
		{ID: "plum", Background: "#121317", Accent: "#51263f", Text: "#e6d8bd"},
		{ID: "azure", Background: "#292929", Accent: "#279fda", Text: "#fafafa"},
		{ID: "charcoal", Background: "#181818", Accent: "#242424", Text: "#fafafa"},

		{ID: "cream", Background: "#f4efe4", Accent: "#47695f", Text: "#23190e"},
		{ID: "paper", Background: "#f9fafe", Accent: "#252728", Text: "#252728"},
		{ID: "mint", Background: "#ffffff", Accent: "#52d187", Text: "#333333"},
		{ID: "steel", Background: "#d4d6d5", Accent: "#40465c", Text: "#333333"},
		{ID: "rose", Background: "#d4d6d5", Accent: "#9b888c", Text: "#333333"},
	}
}

func builtinTheme(id string) (Theme, bool) {
	for _, th := range BuiltinThemes() {
		if th.ID == id {
			return th, true
		}
	}
	return Theme{}, false
}

func (s Settings) ActiveTheme() Theme {
	for _, th := range s.Themes {
		if th.ID == s.Theme && th.Validate() == nil {
			return th
		}
	}
	if th, ok := builtinTheme(s.Theme); ok {
		return th
	}
	th, _ := builtinTheme(DefaultThemeID)
	return th
}

func (t Theme) Validate() error {
	var errs []error
	for name, value := range map[string]string{"background": t.Background, "accent": t.Accent, "text": t.Text} {
		if _, _, _, ok := parseColor(value); !ok {
			errs = append(errs, fmt.Errorf("%s color %q is not #rrggbb", name, value))
		}
	}
	if utf8.RuneCountInString(t.Name) > maxThemeName {
		errs = append(errs, fmt.Errorf("name is longer than %d characters", maxThemeName))
	}
	return errors.Join(errs...)
}

func (t Theme) BackgroundRGB() (r, g, b uint8) {
	r, g, b, _ = parseColor(t.Background)
	return r, g, b
}

func (t Theme) Light() bool {
	return brightness(t.Background) > brightness(t.Text)
}

func brightness(color string) float64 {
	r, g, b, _ := parseColor(color)
	return 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
}

func parseColor(s string) (r, g, b uint8, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n), true
}
