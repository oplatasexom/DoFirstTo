package ui

var texts = map[string]map[string]string{
	"ru": {
		"tray.show":     "Показать задачи",
		"tray.settings": "Настройки…",
		"tray.disable":  "Выключить…",
		"tray.quit":     "Выход",
		"tip.locked":    "DoFirstTo — задачи ждут",
		"tip.night":     "DoFirstTo — пора спать",
		"tip.free":      "DoFirstTo — свободно",
		"panel.title":   "DoFirstTo — настройки",
	},
	"en": {
		"tray.show":     "Show tasks",
		"tray.settings": "Settings…",
		"tray.disable":  "Turn off…",
		"tray.quit":     "Quit",
		"tip.locked":    "DoFirstTo — tasks are waiting",
		"tip.night":     "DoFirstTo — time to sleep",
		"tip.free":      "DoFirstTo — free",
		"panel.title":   "DoFirstTo — settings",
	},
}

func tr(lang, key string) string {
	if s, ok := texts[lang][key]; ok {
		return s
	}
	return texts["ru"][key]
}
