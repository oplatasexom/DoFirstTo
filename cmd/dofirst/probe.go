//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"dofirst/internal/domain"
	"dofirst/internal/platform/win"
	"dofirst/internal/service/guard"
	"dofirst/internal/service/rules"
)

func runProbe(ctx context.Context, out io.Writer, settings domain.Settings, interval time.Duration) error {
	ids := make([]string, len(settings.Groups))
	for i, g := range settings.Groups {
		ids[i] = g.ID
	}
	all, err := rules.Compile(settings.Groups, ids)
	if err != nil {
		fmt.Fprintf(out, "В правилах групп есть ошибки: %v\n", err)
	}
	opts := rules.DefaultOptions()
	opts.Always = append(opts.Always, settings.AlwaysAllowed...)
	open, closed := rules.NewSet(all, opts), rules.NewSet(nil, opts)

	fmt.Fprintln(out, "Переключайте окна — здесь появится, как их видит DoFirstTo. Ctrl+C — выход.")
	fmt.Fprintf(out, "%-8s  %-28s  %-12s  %-12s  %s\n", "время", "программа", "все группы", "без групп", "заголовок")

	insp := win.NewInspector()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var last guard.Window
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			w, err := insp.Foreground()
			if err != nil || w.ID == 0 || (w.ID == last.ID && w.Title == last.Title) {
				continue
			}
			last = w
			exe := w.Exe
			if exe == "" {
				exe = "(не распознана)"
			}
			rw := rules.Window{Exe: w.Exe, Title: w.Title}
			fmt.Fprintf(out, "%-8s  %-28s  %-12s  %-12s  %s\n",
				now.Format(time.TimeOnly), exe, open.Check(rw), closed.Check(rw), w.Title)
		}
	}
}
