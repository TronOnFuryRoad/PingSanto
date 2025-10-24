package certs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistWritesFiles(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{
		Cert: filepath.Join(dir, "client.crt"),
		Key:  filepath.Join(dir, "client.key"),
		CA:   filepath.Join(dir, "ca.pem"),
	}

	resp := &Response{
		CertPEM: []byte("CERT"),
		KeyPEM:  []byte("KEY"),
		CAPEM:   []byte("CA"),
	}

	if err := Persist(paths, resp); err != nil {
		t.Fatalf("Persist returned error: %v", err)
	}

	tests := []struct {
		path string
		want string
	}{
		{paths.Cert, "CERT"},
		{paths.Key, "KEY"},
		{paths.CA, "CA"},
	}

	for _, tc := range tests {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("read file %q: %v", tc.path, err)
		}
		if string(data) != tc.want {
			t.Fatalf("file %q: want %q got %q", tc.path, tc.want, string(data))
		}
	}
}

func TestPersistNoResponse(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{
		Cert: filepath.Join(dir, "client.crt"),
		Key:  filepath.Join(dir, "client.key"),
		CA:   filepath.Join(dir, "ca.pem"),
	}

	if err := Persist(paths, nil); err != nil {
		t.Fatalf("Persist returned error: %v", err)
	}

	for _, path := range []string{paths.Cert, paths.Key, paths.CA} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("expected no file at %q", path)
		}
	}
}

func TestPersistCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{
		Cert: filepath.Join(dir, "nested", "client.crt"),
	}

	resp := &Response{CertPEM: []byte("CERT")}
	if err := Persist(paths, resp); err != nil {
		t.Fatalf("Persist returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "nested", "client.crt")); err != nil {
		t.Fatalf("expected nested client.crt to exist: %v", err)
	}
}
