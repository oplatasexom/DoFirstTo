//go:build windows

// Команда dofirst - приложение DoFirstTo, список задач,
// который стоит между пользователем и компьютером
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"dofirst/internal/config"
	"dofirst/internal/domain"
	"dofirst/internal/logo"
	"dofirst/internal/platform/win"
	"dofirst/internal/service/core"
	"dofirst/internal/service/update"
	"dofirst/internal/storage"
	"dofirst/internal/ui"
)

const (
	// tickInterval как часто проверяется активное окно
	tickInterval = 400 * time.Millisecond
	// dpapiEntropy привязывает ключ данных к этому приложению
	dpapiEntropy = "DoFirstTo/v1"
	// maxLogSize после этого размера журнал работы начинается заново
	maxLogSize = 2 << 20
	// updateCheckDelay пауза перед проверкой обновлений после запуска
	updateCheckDelay = 4 * time.Second
)

func main() {
	os.Exit(run())
}

func run() int {
	autostart := flag.Bool("autostart", false, "запуск вместе с Windows: без окна настроек")
	dataDir := flag.String("data-dir", "", "отдельный каталог данных (режим разработки: автозапуск не трогается)")
	probe := flag.Bool("probe", false, "показывать, как распознаются окна, ничего не блокируя")
	probeFor := flag.Duration("probe-for", 0, "остановить --probe через указанное время (0 — до Ctrl+C)")
	version := flag.Bool("version", false, "показать версию и выйти")
	afterUpdate := flag.Bool(afterUpdateFlag[2:], false, "запуск новой версии после обновления: дождаться выхода прежней")
	flag.Parse()

	if *version || *probe {
		win.AttachParentConsole()
	}
	if *version {
		fmt.Println(config.AppName, config.Version)
		return 0
	}
	if *probe {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if *probeFor > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, *probeFor)
			defer cancel()
		}
		if err := runProbe(ctx, os.Stdout, domain.DefaultSettings(), tickInterval); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if err := runApp(*autostart || *afterUpdate, *afterUpdate, *dataDir); err != nil {
		win.MessageBox(config.AppName, err.Error(), win.MessageBoxError)
		return 1
	}
	return 0
}

// runApp собирает зависимости и запускает приложение. quiet - запуск без
// окна настроек (автозапуск, перезапуск после обновления)
func runApp(quiet, afterUpdate bool, dataDir string) error {
	paths, err := config.Resolve()
	if err != nil {
		return err
	}
	dev := dataDir != ""
	name := config.AppName
	if dev {
		paths = config.Paths{Data: dataDir, Log: filepath.Join(dataDir, "dofirst.log"), WebView: filepath.Join(dataDir, "webview")}
		name += ".dev"
	}

	instance, first, err := win.AcquireInstance(name)
	for wait := 15 * time.Second; afterUpdate && err == nil && !first && wait > 0; wait -= 200 * time.Millisecond {
		time.Sleep(200 * time.Millisecond)
		instance, first, err = win.AcquireInstance(name)
	}
	if err != nil {
		return err
	}
	if !first {
		win.WakeRunningInstance(name)
		return nil
	}
	defer instance.Release()

	if err := os.MkdirAll(paths.Data, 0o700); err != nil {
		return fmt.Errorf("не удалось создать каталог данных: %w", err)
	}
	logger, closeLog := openLog(paths.Log)
	defer closeLog()
	logger.Info("start", "version", config.Version, "quiet", quiet, "dev", dev)

	if _, err := win.WebViewVersion(); err != nil {
		logger.Error("webview2 runtime", "err", err)
		return errors.New("Не найден Microsoft Edge WebView2 Runtime.\n\n" +
			"Он входит в Windows 11 и обычно уже установлен в Windows 10. " +
			"Установите его с сайта Microsoft и запустите DoFirstTo снова.")
	}

	vault, err := storage.Open(paths.Data, win.NewDPAPI(dpapiEntropy))
	if err != nil {
		logger.Error("open storage", "err", err)
		if errors.Is(err, storage.ErrKeyUnavailable) {
			return fmt.Errorf("Данные DoFirstTo зашифрованы для другой учётной записи Windows или другого компьютера "+
				"и здесь не открываются.\n\nЧтобы начать заново, удалите папку:\n%s", paths.Data)
		}
		return fmt.Errorf("не удалось открыть хранилище: %w", err)
	}

	loop, err := win.NewLoop(name, uint32(logo.Espresso.B)<<16|uint32(logo.Espresso.G)<<8|uint32(logo.Espresso.R))
	if err != nil {
		return fmt.Errorf("не удалось создать окно: %w", err)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("не удалось определить путь к программе: %w", err)
	}
	update.Cleanup(exe)
	updates, err := newUpdater(exe)
	if err != nil {
		logger.Error("updates are disabled", "err", err)
	}

	var startup *win.Autostart
	if !dev {
		startup = &win.Autostart{Key: win.RunKey, Name: config.AppName, Command: fmt.Sprintf(`"%s" --autostart`, exe)}
	}

	inspector := win.NewInspector()
	app := ui.New(ui.Options{
		Loop: loop, Log: logger, Inspector: inspector, Autostart: startup,
		WebViewDir: paths.WebView, Version: config.Version, Tick: tickInterval,
	})
	c, err := core.New(core.Deps{
		Store: vault, Inspector: inspector, Controller: win.Controller{},
		Screen: app, Now: time.Now, Log: logger,
	})
	if err != nil {
		logger.Error("load state", "err", err)
		return fmt.Errorf("не удалось прочитать сохранённые данные: %w", err)
	}
	if updates == nil {
		app.Attach(c, ui.NoUpdates{})
	} else {
		app.Attach(c, updates)
		go func() {
			time.Sleep(updateCheckDelay)
			info, err := updates.Check(context.Background())
			if err != nil {
				logger.Warn("update check", "err", err)
				return
			}
			if info.Version != "" {
				app.OfferUpdate(info)
			}
		}()
	}

	app.Run(!quiet)
	logger.Info("stop")
	return nil
}

func openLog(path string) (*slog.Logger, func()) {
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		_ = os.Remove(path)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.SetOutput(io.Discard)
		return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}
	}
	log.SetOutput(file)
	return slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})), func() { _ = file.Close() }
}
