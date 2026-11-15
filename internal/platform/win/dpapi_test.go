//go:build windows

package win

import (
	"bytes"
	"testing"
)

func TestDPAPIRoundTrip(t *testing.T) {
	d := NewDPAPI("dofirst-test")
	secret := []byte("0123456789abcdef0123456789abcdef")

	sealed, err := d.Protect(secret)
	if err != nil {
		t.Fatalf("Protect: %v", err)
	}
	if bytes.Contains(sealed, secret) {
		t.Error("sealed blob contains the plaintext")
	}

	got, err := d.Unprotect(sealed)
	if err != nil {
		t.Fatalf("Unprotect: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Errorf("Unprotect = %q, want %q", got, secret)
	}
}

func TestDPAPIErrors(t *testing.T) {
	d := NewDPAPI("dofirst-test")
	sealed, err := d.Protect([]byte("secret"))
	if err != nil {
		t.Fatalf("Protect: %v", err)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "empty plaintext", run: func() error { _, err := d.Protect(nil); return err }},
		{name: "empty blob", run: func() error { _, err := d.Unprotect(nil); return err }},
		{name: "garbage blob", run: func() error { _, err := d.Unprotect([]byte("not a dpapi blob")); return err }},
		{
			name: "other entropy",
			run:  func() error { _, err := NewDPAPI("another-app").Unprotect(sealed); return err },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err == nil {
				t.Error("got nil error, want failure")
			}
		})
	}
}
