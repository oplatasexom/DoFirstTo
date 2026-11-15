package ui

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed web/index.html web/app.css web/core.js web/i18n.js web/stage.js web/panel.js web/menu.js
var assets embed.FS

// Виды окон
const (
	// экран задач, ночной экран и виджет
	WindowStage = "stage"
	// настройки, журнал и статистика
	WindowPanel = "panel"
	// меню значка в трее
	WindowMenu = "menu"
)

func Page(window, extraScript string) (string, error) {
	read := func(name string) (string, error) {
		data, err := assets.ReadFile("web/" + name)
		if err != nil {
			return "", fmt.Errorf("read asset %s: %w", name, err)
		}
		return string(data), nil
	}

	html, err := read("index.html")
	if err != nil {
		return "", err
	}
	css, err := read("app.css")
	if err != nil {
		return "", err
	}
	var js strings.Builder
	for _, name := range []string{"i18n.js", "core.js", "stage.js", "panel.js", "menu.js"} {
		src, err := read(name)
		if err != nil {
			return "", err
		}
		js.WriteString(src)
		js.WriteString("\n")
	}

	boot := fmt.Sprintf("window.__dofirstWindow=%q;\n%s", window, extraScript)
	for _, part := range [][2]string{{"/*CSS*/", css}, {"/*BOOT*/", boot}, {"/*JS*/", js.String()}} {
		before, after, found := strings.Cut(html, part[0])
		if !found {
			return "", fmt.Errorf("page template: marker %s is missing", part[0])
		}
		html = before + part[1] + after
	}
	return html, nil
}
