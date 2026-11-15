package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const asset = "dofirst-windows-amd64.exe"

type github struct {
	tag       string
	binary    []byte
	signature string
	noAsset   bool
	status    int
	server    *httptest.Server
}

func (g *github) start(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/dofirst/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		if g.status != 0 {
			http.Error(w, "limit", g.status)
			return
		}
		type item struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		assets := []item{{asset + ".sig", g.server.URL + "/dl/sig"}, {"notes.txt", g.server.URL + "/dl/notes"}}
		if !g.noAsset {
			assets = append(assets, item{asset, g.server.URL + "/dl/exe"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": g.tag, "assets": assets})
	})
	mux.HandleFunc("/dl/exe", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(g.binary) })
	mux.HandleFunc("/dl/sig", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(g.signature + "\n")) })
	g.server = httptest.NewServer(mux)
	t.Cleanup(g.server.Close)
}

type fixture struct {
	gh      *github
	svc     *Service
	exe     string
	private ed25519.PrivateKey
}

func newFixture(t *testing.T, current string, mutate func(*github, *Config)) *fixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "dofirst.exe")
	if err := os.WriteFile(exe, []byte("old build"), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gh := &github{tag: "v1.3.0", binary: []byte("new build")}
	gh.signature = Sign(private, gh.binary)
	gh.start(t)
	cfg := Config{
		Repo: "owner/dofirst", PublicKey: base64.StdEncoding.EncodeToString(public),
		Current: current, Asset: asset, ExePath: exe, APIBase: gh.server.URL,
	}
	if mutate != nil {
		mutate(gh, &cfg)
	}
	svc, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &fixture{gh: gh, svc: svc, exe: exe, private: private}
}

func TestNewer(t *testing.T) {
	tests := []struct {
		candidate, current string
		want               bool
	}{
		{"1.3.0", "1.2.9", true},
		{"v1.3.0", "1.3.0", false},
		{"1.2.0", "1.3.0", false},
		{"1.10.0", "1.9.0", true},
		{"2.0", "1.9.9", true},
		{"1.2", "1.2.0", false},
		{"0.1.0", "0.1.0-dev", true},
		{"0.1.0-rc1", "0.1.0", false},
		{"0.2.0-rc1", "0.1.0", true},
		{"", "1.0.0", false},
		{"latest", "1.0.0", false},
		{"1.x.0", "1.0.0", false},
		{"1.0.1", "unknown", true},
	}
	for _, tt := range tests {
		if got := Newer(tt.candidate, tt.current); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tt.candidate, tt.current, got, tt.want)
		}
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		mutate    func(*github, *Config)
		wantNewer bool
		wantErr   error
		anyErr    bool
	}{
		{name: "newer release", current: "1.2.0", wantNewer: true},
		{name: "same version", current: "1.3.0"},
		{name: "local build is ahead", current: "2.0.0"},
		{name: "dev build of the same version", current: "1.3.0-dev", wantNewer: true},
		{name: "no build for this system", current: "1.2.0", mutate: func(g *github, _ *Config) { g.noAsset = true }, wantErr: ErrNoAsset},
		{name: "api failure", current: "1.2.0", mutate: func(g *github, _ *Config) { g.status = http.StatusForbidden }, anyErr: true},
		{name: "no repository", current: "1.2.0", mutate: func(_ *github, c *Config) { c.Repo = "" }, wantErr: ErrNotConfigured},
		{name: "no key", current: "1.2.0", mutate: func(_ *github, c *Config) { c.PublicKey = "" }, wantErr: ErrNotConfigured},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, tt.current, tt.mutate)
			rel, newer, err := f.svc.Check(context.Background())
			switch {
			case tt.anyErr && err == nil:
				t.Fatal("Check = nil error, want failure")
			case !tt.anyErr && !errors.Is(err, tt.wantErr):
				t.Fatalf("Check error = %v, want %v", err, tt.wantErr)
			}
			if newer != tt.wantNewer {
				t.Errorf("newer = %v, want %v", newer, tt.wantNewer)
			}
			if newer && (rel.Version != "1.3.0" || rel.AssetURL == "" || rel.SignatureURL == "") {
				t.Errorf("release = %+v, want version 1.3.0 with both links", rel)
			}
		})
	}
}

func TestApply(t *testing.T) {
	_, stranger, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*github, *Config)
		wantErr error
	}{
		{name: "signed release is installed"},
		{
			name:    "tampered binary",
			mutate:  func(g *github, _ *Config) { g.binary = []byte("new build with a backdoor") },
			wantErr: ErrBadSignature,
		},
		{
			name:    "signed by another key",
			mutate:  func(g *github, _ *Config) { g.signature = Sign(stranger, g.binary) },
			wantErr: ErrBadSignature,
		},
		{
			name:    "signature is garbage",
			mutate:  func(g *github, _ *Config) { g.signature = "not base64 !!!" },
			wantErr: ErrBadSignature,
		},
		{
			name:    "empty signature",
			mutate:  func(g *github, _ *Config) { g.signature = "" },
			wantErr: ErrBadSignature,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, "1.2.0", tt.mutate)
			rel, newer, err := f.svc.Check(context.Background())
			if err != nil || !newer {
				t.Fatalf("Check = %v, %v; want a newer release", newer, err)
			}

			err = f.svc.Apply(context.Background(), rel)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply = %v, want %v", err, tt.wantErr)
			}

			got, readErr := os.ReadFile(f.exe)
			if readErr != nil {
				t.Fatalf("ReadFile: %v", readErr)
			}
			if tt.wantErr != nil {
				if string(got) != "old build" {
					t.Errorf("executable changed after a failed update: %q", got)
				}
				if _, statErr := os.Stat(f.exe + ".new"); statErr == nil {
					t.Error("unverified download was left on disk")
				}
				return
			}
			if string(got) != "new build" {
				t.Errorf("executable = %q, want the new build", got)
			}
			if old, _ := os.ReadFile(f.exe + ".old"); string(old) != "old build" {
				t.Errorf("previous build was not kept aside: %q", old)
			}
			Cleanup(f.exe)
			if _, statErr := os.Stat(f.exe + ".old"); statErr == nil {
				t.Error("Cleanup left the previous build")
			}
		})
	}
}

func TestApplyNeedsConfiguration(t *testing.T) {
	f := newFixture(t, "1.2.0", func(_ *github, c *Config) { c.PublicKey = "" })
	if f.svc.Configured() {
		t.Error("Configured = true without a key")
	}
	err := f.svc.Apply(context.Background(), Release{AssetURL: f.gh.server.URL + "/dl/exe", SignatureURL: f.gh.server.URL + "/dl/sig"})
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Apply without a key = %v, want %v", err, ErrNotConfigured)
	}
}

func TestApplyDownloadFailure(t *testing.T) {
	f := newFixture(t, "1.2.0", nil)
	err := f.svc.Apply(context.Background(), Release{AssetURL: f.gh.server.URL + "/dl/missing", SignatureURL: f.gh.server.URL + "/dl/sig"})
	if err == nil {
		t.Fatal("Apply = nil, want a download error")
	}
	if got, _ := os.ReadFile(f.exe); string(got) != "old build" {
		t.Errorf("executable changed after a failed download: %q", got)
	}
}

func TestCancelledContext(t *testing.T) {
	f := newFixture(t, "1.2.0", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := f.svc.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Check with cancelled context = %v, want %v", err, context.Canceled)
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	for _, key := range []string{"not base64 !!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := New(Config{Repo: "owner/dofirst", PublicKey: key}); err == nil {
			t.Errorf("New with key %q = nil error, want failure", key)
		}
	}
}
