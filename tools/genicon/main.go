//	go run ./tools/genicon -out build/icons
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"dofirst/internal/logo"
)

func main() {
	out := flag.String("out", "build/icons", "каталог для значков")
	flag.Parse()

	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	files := []struct {
		name  string
		size  int
		style logo.Style
	}{
		{"app-256.png", 256, logo.App},
		{"app-48.png", 48, logo.App},
		{"app-16.png", 16, logo.App},
		{"tray-on-dark-32.png", 32, logo.TrayOnDark},
		{"tray-on-light-32.png", 32, logo.TrayOnLight},
	}
	for _, f := range files {
		data, err := logo.PNG(f.size, f.style)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Println(path)
	}
	return nil
}
