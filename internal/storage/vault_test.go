package storage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeProtector struct {
	user string
	fail error
}

var fakeTag = []byte("fake:")

func (p fakeProtector) Protect(plain []byte) ([]byte, error) {
	if p.fail != nil {
		return nil, p.fail
	}
	out := append([]byte{}, fakeTag...)
	out = append(out, []byte(p.user+":")...)
	for _, b := range plain {
		out = append(out, b^0x5a)
	}
	return out, nil
}

func (p fakeProtector) Unprotect(blob []byte) ([]byte, error) {
	if p.fail != nil {
		return nil, p.fail
	}
	prefix := append(append([]byte{}, fakeTag...), []byte(p.user+":")...)
	if !bytes.HasPrefix(blob, prefix) {
		return nil, errors.New("blob belongs to another user")
	}
	body := blob[len(prefix):]
	out := make([]byte, len(body))
	for i, b := range body {
		out[i] = b ^ 0x5a
	}
	return out, nil
}

type doc struct {
	Title string   `json:"title"`
	Notes []string `json:"notes"`
	Count int      `json:"count"`
}

func openVault(t *testing.T, dir string) *Vault {
	t.Helper()
	v, err := Open(dir, fakeProtector{user: "anna"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return v
}

func TestSaveLoadRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		doc  doc
	}{
		{name: "settings", doc: doc{Title: "Учиться по roadmap", Notes: []string{"каналы", "контексты"}, Count: 3}},
		{name: "empty-doc", doc: doc{}},
		{name: "journal_2026", doc: doc{Title: "секрет", Count: -1}},
	}
	dir := t.TempDir()
	v := openVault(t, dir)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Save(tt.name, tt.doc); err != nil {
				t.Fatalf("Save: %v", err)
			}
			var got doc
			if err := v.Load(tt.name, &got); err != nil {
				t.Fatalf("Load: %v", err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.doc) {
				t.Errorf("Load = %+v, want %+v", got, tt.doc)
			}
		})
	}
}

func TestDataSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	if err := openVault(t, dir).Save("settings", doc{Title: "утро"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var got doc
	if err := openVault(t, dir).Load("settings", &got); err != nil {
		t.Fatalf("Load after reopen: %v", err)
	}
	if got.Title != "утро" {
		t.Errorf("Title = %q, want %q", got.Title, "утро")
	}
}

func TestFilesAreEncrypted(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)
	secret := "очень личная заметка"
	if err := v.Save("journal", doc{Title: secret}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "journal"+docExt))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("title")) {
		t.Error("document file contains plaintext")
	}

	if err := v.Save("journal", doc{Title: secret}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := os.ReadFile(filepath.Join(dir, "journal"+docExt))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Equal(raw, again) {
		t.Error("nonce is reused between saves")
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string, v *Vault) // портит хранилище
		load    string
		wantErr error
	}{
		{
			name:    "missing document",
			prepare: func(*testing.T, string, *Vault) {},
			load:    "settings",
			wantErr: ErrNotFound,
		},
		{
			name: "flipped byte",
			prepare: func(t *testing.T, dir string, _ *Vault) {
				path := filepath.Join(dir, "settings"+docExt)
				raw := mustRead(t, path)
				raw[len(raw)-1] ^= 0xff
				mustWrite(t, path, raw)
			},
			load:    "settings",
			wantErr: ErrCorrupt,
		},
		{
			name: "truncated file",
			prepare: func(t *testing.T, dir string, _ *Vault) {
				mustWrite(t, filepath.Join(dir, "settings"+docExt), []byte("DFX"))
			},
			load:    "settings",
			wantErr: ErrCorrupt,
		},
		{
			name: "plain json instead of ciphertext",
			prepare: func(t *testing.T, dir string, _ *Vault) {
				mustWrite(t, filepath.Join(dir, "settings"+docExt), []byte(`{"title":"подделка","count":1}`))
			},
			load:    "settings",
			wantErr: ErrCorrupt,
		},
		{
			name: "document copied under another name",
			prepare: func(t *testing.T, dir string, _ *Vault) {
				raw := mustRead(t, filepath.Join(dir, "settings"+docExt))
				mustWrite(t, filepath.Join(dir, "stats"+docExt), raw)
			},
			load:    "stats",
			wantErr: ErrCorrupt,
		},
		{
			name:    "path traversal",
			prepare: func(*testing.T, string, *Vault) {},
			load:    `..\secret`,
			wantErr: ErrBadName,
		},
		{
			name:    "upper case name",
			prepare: func(*testing.T, string, *Vault) {},
			load:    "Settings",
			wantErr: ErrBadName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			v := openVault(t, dir)
			if err := v.Save("settings", doc{Title: "утро"}); err != nil {
				t.Fatalf("Save: %v", err)
			}
			if tt.name == "missing document" {
				if err := v.Delete("settings"); err != nil {
					t.Fatalf("Delete: %v", err)
				}
			}
			tt.prepare(t, dir, v)

			var got doc
			err := v.Load(tt.load, &got)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Load(%q) = %v, want %v", tt.load, err, tt.wantErr)
			}
		})
	}
}

func TestOpenKeyErrors(t *testing.T) {
	boom := errors.New("dpapi is down")

	tests := []struct {
		name    string
		prepare func(t *testing.T, dir string)
		kp      KeyProtector
		wantErr error
	}{
		{
			name:    "key of another user",
			prepare: func(t *testing.T, dir string) { openVault(t, dir) },
			kp:      fakeProtector{user: "boris"},
			wantErr: ErrKeyUnavailable,
		},
		{
			name: "key of wrong size",
			prepare: func(t *testing.T, dir string) {
				blob, err := fakeProtector{user: "anna"}.Protect([]byte("short"))
				if err != nil {
					t.Fatalf("Protect: %v", err)
				}
				mustWrite(t, filepath.Join(dir, keyFile), blob)
			},
			kp:      fakeProtector{user: "anna"},
			wantErr: ErrKeyUnavailable,
		},
		{
			name:    "protector fails on first run",
			prepare: func(*testing.T, string) {},
			kp:      fakeProtector{user: "anna", fail: boom},
			wantErr: boom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.prepare(t, dir)
			if _, err := Open(dir, tt.kp); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Open = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLostKeyMakesDocumentsUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := openVault(t, dir).Save("settings", doc{Title: "утро"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, keyFile)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	var got doc
	if err := openVault(t, dir).Load("settings", &got); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Load with new key = %v, want %v", err, ErrCorrupt)
	}
}

func TestDelete(t *testing.T) {
	v := openVault(t, t.TempDir())
	if err := v.Delete("settings"); err != nil {
		t.Errorf("Delete of missing document = %v, want nil", err)
	}
	if err := v.Delete("../x"); !errors.Is(err, ErrBadName) {
		t.Errorf("Delete with bad name = %v, want %v", err, ErrBadName)
	}
}

func TestSaveRejectsUnencodableValue(t *testing.T) {
	v := openVault(t, t.TempDir())
	if err := v.Save("settings", func() {}); err == nil {
		t.Error("Save of a func = nil, want error")
	}
	if err := v.Save("bad name", doc{}); !errors.Is(err, ErrBadName) {
		t.Errorf("Save with bad name = %v, want %v", err, ErrBadName)
	}
}

func TestConcurrentSaves(t *testing.T) {
	dir := t.TempDir()
	v := openVault(t, dir)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := v.Save("stats", doc{Count: i}); err != nil {
				t.Errorf("Save: %v", err)
			}
			var got doc
			if err := v.Load("stats", &got); err != nil {
				t.Errorf("Load: %v", err)
			}
		}()
	}
	wg.Wait()

	left, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return raw
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
