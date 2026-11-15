package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Ошибки обновления
var (
	// в сборке не указан репозиторий или ключ подписи.
	ErrNotConfigured = errors.New("updates are not configured")
	// подпись скачанного файла не сошлась
	ErrBadSignature = errors.New("signature does not match")
	// в релизе нет файла для этой архитектуры.
	ErrNoAsset = errors.New("release has no build for this system")
)

const (
	// ограничивает размер скачиваемого файла
	maxDownload = 200 << 20
	// с запасом покрывает подпись в base64
	maxSignature = 4 << 10

	apiBase = "https://api.github.com"
)

type Release struct {
	Version string
	AssetURL string
	SignatureURL string
}

type Config struct {
	Repo string
	PublicKey string
	Current string
	Asset string
	ExePath string
	Client *http.Client
	APIBase string
}

type Service struct {
	cfg Config
	key ed25519.PublicKey
}

func New(cfg Config) (*Service, error) {
	s := &Service{cfg: cfg}
	if s.cfg.Client == nil {
		s.cfg.Client = http.DefaultClient
	}
	if s.cfg.APIBase == "" {
		s.cfg.APIBase = apiBase
	}
	if cfg.PublicKey != "" {
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.PublicKey))
		if err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("update public key: want %d bytes in base64", ed25519.PublicKeySize)
		}
		s.key = key
	}
	return s, nil
}

func (s *Service) Configured() bool {
	return s.cfg.Repo != "" && s.key != nil
}

func (s *Service) Check(ctx context.Context) (Release, bool, error) {
	if !s.Configured() {
		return Release{}, false, ErrNotConfigured
	}
	var payload struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	body, err := s.get(ctx, s.cfg.APIBase+"/repos/"+s.cfg.Repo+"/releases/latest", 1<<20)
	if err != nil {
		return Release{}, false, fmt.Errorf("latest release: %w", err)
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Release{}, false, fmt.Errorf("latest release: %w", err)
	}

	rel := Release{Version: strings.TrimPrefix(payload.Tag, "v")}
	if !Newer(rel.Version, s.cfg.Current) {
		return rel, false, nil
	}
	for _, a := range payload.Assets {
		switch a.Name {
		case s.cfg.Asset:
			rel.AssetURL = a.URL
		case s.cfg.Asset + ".sig":
			rel.SignatureURL = a.URL
		}
	}
	if rel.AssetURL == "" || rel.SignatureURL == "" {
		return rel, false, fmt.Errorf("%w: %s", ErrNoAsset, s.cfg.Asset)
	}
	return rel, true, nil
}

func (s *Service) Apply(ctx context.Context, rel Release) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	binary, err := s.get(ctx, rel.AssetURL, maxDownload)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	rawSig, err := s.get(ctx, rel.SignatureURL, maxSignature)
	if err != nil {
		return fmt.Errorf("download signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(rawSig)))
	if err != nil {
		return fmt.Errorf("%w: signature is not base64", ErrBadSignature)
	}
	if !ed25519.Verify(s.key, binary, sig) {
		return ErrBadSignature
	}

	fresh, old := s.cfg.ExePath+".new", s.cfg.ExePath+".old"
	if err := os.WriteFile(fresh, binary, 0o755); err != nil {
		return fmt.Errorf("write update: %w", err)
	}
	_ = os.Remove(old)
	if err := os.Rename(s.cfg.ExePath, old); err != nil {
		_ = os.Remove(fresh)
		return fmt.Errorf("move current version aside: %w", err)
	}
	if err := os.Rename(fresh, s.cfg.ExePath); err != nil {
		_ = os.Rename(old, s.cfg.ExePath)
		_ = os.Remove(fresh)
		return fmt.Errorf("install update: %w", err)
	}
	return nil
}

func Cleanup(exePath string) {
	_ = os.Remove(exePath + ".old")
}

func (s *Service) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	req.Header.Set("User-Agent", "dofirst-updater")
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: response is larger than %d bytes", url, limit)
	}
	return data, nil
}

func Newer(candidate, current string) bool {
	cNums, cPre := parseVersion(candidate)
	oNums, oPre := parseVersion(current)
	if cNums == nil {
		return false
	}
	for i := range max(len(cNums), len(oNums)) {
		var a, b int
		if i < len(cNums) {
			a = cNums[i]
		}
		if i < len(oNums) {
			b = oNums[i]
		}
		if a != b {
			return a > b
		}
	}
	return !cPre && oPre
}

func parseVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, _, pre := strings.Cut(v, "-")
	if core == "" {
		return nil, false
	}
	var nums []int
	for _, part := range strings.Split(core, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, pre
}

func Sign(private ed25519.PrivateKey, data []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(private, data))
}
