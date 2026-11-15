package catalog

import "strings"

type Category string

// Разделы каталога
const (
	Browsers Category = "browsers"
	Dev      Category = "dev"
	AI       Category = "ai"
	Docs     Category = "docs"
	Study    Category = "study"
	Music    Category = "music"
	Chat     Category = "chat"
	Design   Category = "design"
	Other    Category = "other"
)

// Categories возвращает разделы в порядке показа
func Categories() []Category {
	return []Category{Browsers, Dev, AI, Docs, Study, Music, Chat, Design, Other}
}

// программа каталога
type App struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category Category `json:"category"`
	//имена исполняемых файлов в нижнем регистре
	Exes []string `json:"exes"`
}

type Site struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category Category `json:"category"`
	Titles   []string `json:"titles"`
}

func Apps() []App {
	return []App{
		{"chrome", "Google Chrome", Browsers, []string{"chrome.exe"}},
		{"edge", "Microsoft Edge", Browsers, []string{"msedge.exe"}},
		{"firefox", "Firefox", Browsers, []string{"firefox.exe"}},
		{"zen", "Zen Browser", Browsers, []string{"zen.exe"}},
		{"yandex", "Яндекс Браузер", Browsers, []string{"browser.exe"}},
		{"opera", "Opera", Browsers, []string{"opera.exe"}},
		{"brave", "Brave", Browsers, []string{"brave.exe"}},
		{"vivaldi", "Vivaldi", Browsers, []string{"vivaldi.exe"}},

		{"vscode", "Visual Studio Code", Dev, []string{"code.exe"}},
		{"cursor", "Cursor", Dev, []string{"cursor.exe"}},
		{"visualstudio", "Visual Studio", Dev, []string{"devenv.exe"}},
		{"goland", "GoLand", Dev, []string{"goland64.exe"}},
		{"idea", "IntelliJ IDEA", Dev, []string{"idea64.exe"}},
		{"pycharm", "PyCharm", Dev, []string{"pycharm64.exe"}},
		{"webstorm", "WebStorm", Dev, []string{"webstorm64.exe"}},
		{"rider", "Rider", Dev, []string{"rider64.exe"}},
		{"androidstudio", "Android Studio", Dev, []string{"studio64.exe"}},
		{"terminal", "Терминал и PowerShell", Dev, []string{"windowsterminal.exe", "powershell.exe", "pwsh.exe", "cmd.exe"}},
		{"gitbash", "Git Bash", Dev, []string{"mintty.exe", "git-bash.exe"}},
		{"docker", "Docker Desktop", Dev, []string{"docker desktop.exe"}},
		{"postman", "Postman", Dev, []string{"postman.exe"}},
		{"dbeaver", "DBeaver", Dev, []string{"dbeaver.exe"}},
		{"githubdesktop", "GitHub Desktop", Dev, []string{"githubdesktop.exe"}},
		{"sublime", "Sublime Text", Dev, []string{"sublime_text.exe"}},
		{"notepadpp", "Notepad++", Dev, []string{"notepad++.exe"}},

		{"claude", "Claude", AI, []string{"claude.exe"}},
		{"chatgpt", "ChatGPT", AI, []string{"chatgpt.exe"}},
		{"perplexity", "Perplexity", AI, []string{"perplexity.exe"}},

		{"obsidian", "Obsidian", Docs, []string{"obsidian.exe"}},
		{"notion", "Notion", Docs, []string{"notion.exe"}},
		{"word", "Word", Docs, []string{"winword.exe"}},
		{"excel", "Excel", Docs, []string{"excel.exe"}},
		{"powerpoint", "PowerPoint", Docs, []string{"powerpnt.exe"}},
		{"onenote", "OneNote", Docs, []string{"onenote.exe"}},
		{"notepad", "Блокнот", Docs, []string{"notepad.exe"}},
		{"acrobat", "Adobe Acrobat", Docs, []string{"acrobat.exe", "acrord32.exe"}},
		{"sumatra", "SumatraPDF", Docs, []string{"sumatrapdf.exe"}},
		{"anki", "Anki", Study, []string{"anki.exe"}},

		{"spotify", "Spotify", Music, []string{"spotify.exe"}},
		{"yandexmusic", "Яндекс Музыка", Music, []string{"яндекс музыка.exe"}},
		{"applemusic", "Apple Music", Music, []string{"applemusic.exe"}},
		{"aimp", "AIMP", Music, []string{"aimp.exe"}},
		{"foobar", "foobar2000", Music, []string{"foobar2000.exe"}},
		{"vlc", "VLC", Music, []string{"vlc.exe"}},

		{"telegram", "Telegram", Chat, []string{"telegram.exe"}},
		{"discord", "Discord", Chat, []string{"discord.exe"}},
		{"slack", "Slack", Chat, []string{"slack.exe"}},
		{"teams", "Microsoft Teams", Chat, []string{"ms-teams.exe", "ms-teams_modulehost.exe", "teams.exe"}},
		{"zoom", "Zoom", Chat, []string{"zoom.exe"}},
		{"whatsapp", "WhatsApp", Chat, []string{"whatsapp.exe", "whatsapp.root.exe"}},

		{"figma", "Figma", Design, []string{"figma.exe"}},
		{"photoshop", "Photoshop", Design, []string{"photoshop.exe"}},
		{"blender", "Blender", Design, []string{"blender.exe"}},
	}
}

func Sites() []Site {
	return []Site{
		{"claude", "Claude", AI, []string{"Claude"}},
		{"chatgpt", "ChatGPT", AI, []string{"ChatGPT"}},
		{"deepseek", "DeepSeek", AI, []string{"DeepSeek"}},
		{"perplexity", "Perplexity", AI, []string{"Perplexity"}},
		{"gemini", "Gemini", AI, []string{"Gemini"}},

		{"github", "GitHub", Dev, []string{"GitHub"}},
		{"stackoverflow", "Stack Overflow", Dev, []string{"Stack Overflow"}},
		{"mdn", "MDN", Dev, []string{"MDN"}},
		{"godocs", "Документация Go", Dev, []string{"Go Packages", "The Go Programming Language", "A Tour of Go", "Go by Example"}},
		{"localhost", "localhost", Dev, []string{"localhost"}},

		{"roadmap", "roadmap.sh", Study, []string{"roadmap.sh"}},
		{"leetcode", "LeetCode", Study, []string{"LeetCode"}},
		{"stepik", "Stepik", Study, []string{"Stepik"}},
		{"coursera", "Coursera", Study, []string{"Coursera"}},

		{"gdocs", "Google Документы", Docs, []string{"Google Docs", "Google Документы", "Google Sheets", "Google Таблицы"}},
		{"pdf", "PDF-файлы", Docs, []string{".pdf"}},
	}
}

func Find(exe string) (App, bool) {
	exe = strings.ToLower(strings.TrimSpace(exe))
	for _, app := range Apps() {
		for _, name := range app.Exes {
			if name == exe {
				return app, true
			}
		}
	}
	return App{}, false
}
