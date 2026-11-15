//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"dofirst/internal/config"
	"dofirst/internal/service/update"
	"dofirst/internal/ui"
)

const (
	checkTimeout = 20 * time.Second
	applyTimeout = 5 * time.Minute
	afterUpdateFlag = "--after-update"
)

type updater struct {
	svc *update.Service
	exe string

	mu    sync.Mutex
	found *update.Release
}

func newUpdater(exe string) (*updater, error) {
	svc, err := update.New(update.Config{
		Repo:      config.UpdateRepo,
		PublicKey: config.UpdateKey,
		Current:   config.Version,
		Asset:     fmt.Sprintf("dofirst-windows-%s.exe", runtime.GOARCH),
		ExePath:   exe,
	})
	if err != nil {
		return nil, err
	}
	return &updater{svc: svc, exe: exe}, nil
}

// Check ищет новую версию
func (u *updater) Check(ctx context.Context) (ui.UpdateInfo, error) {
	if !u.svc.Configured() {
		return ui.UpdateInfo{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	rel, newer, err := u.svc.Check(ctx)
	if err != nil {
		return ui.UpdateInfo{Configured: true}, err
	}
	if !newer {
		return ui.UpdateInfo{Configured: true}, nil
	}
	u.mu.Lock()
	u.found = &rel
	u.mu.Unlock()
	return ui.UpdateInfo{Configured: true, Version: rel.Version}, nil
}

// Apply устанавливает найденную версию и запускает ее
func (u *updater) Apply(ctx context.Context) error {
	u.mu.Lock()
	rel := u.found
	u.mu.Unlock()
	if rel == nil {
		return errors.New("no update was found")
	}
	ctx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()

	if err := u.svc.Apply(ctx, *rel); err != nil {
		return err
	}
	if err := exec.Command(u.exe, afterUpdateFlag).Start(); err != nil {
		return fmt.Errorf("start the new version: %w", err)
	}
	return nil
}
