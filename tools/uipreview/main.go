package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"dofirst/internal/domain"
	"dofirst/internal/ui"
)

//go:embed mock.js
var mock string

func main() {
	addr := flag.String("addr", "localhost:4173", "адрес сервера предпросмотра")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		window := r.URL.Query().Get("window")
		if window != ui.WindowPanel && window != ui.WindowMenu {
			window = ui.WindowStage
		}
		// Готовые темы подставной мост берёт у приложения, а не хранит копию.
		themes, err := json.Marshal(domain.BuiltinThemes())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		page, err := ui.Page(window, fmt.Sprintf("window.__dofirstThemes=%s;\n%s", themes, mock))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, page)
	})

	log.Printf("предпросмотр: http://%s/?window=stage&scene=tasks", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
