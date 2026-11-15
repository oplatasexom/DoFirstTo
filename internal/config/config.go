package config

import (
	"fmt"
	"os"
	"path/filepath"
)

//	go build -ldflags "-X dofirst/internal/config.Version=1.2.3"
var (
	Version = "0.1.0-dev"
	UpdateRepo = ""
	// Ed25519 (base64) для проверки подписи релиза
	UpdateKey = ""
)

const (
	AppName = "DoFirstTo"
	dirName = "DoFirst"
)

type Paths struct {
	Data string
	Log string
	WebView string
}

func Resolve() (Paths, error) {
	roaming, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("locate config dir: %w", err)
	}
	local, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, fmt.Errorf("locate cache dir: %w", err)
	}
	data := filepath.Join(roaming, dirName)
	return Paths{
		Data:    data,
		Log:     filepath.Join(data, "dofirst.log"),
		WebView: filepath.Join(local, dirName, "webview"),
	}, nil
}
