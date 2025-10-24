package certs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientCertExpiry(t *testing.T) {
	caCert, caKey := mustCreateCA(t)
	clientCert, _ := mustCreateClientCert(t, caCert, caKey)

	dir := t.TempDir()
	path := filepath.Join(dir, "client.pem")
	if err := os.WriteFile(path, clientCert, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	expiry, err := ClientCertExpiry(path)
	if err != nil {
		t.Fatalf("ClientCertExpiry: %v", err)
	}
	if time.Until(expiry) <= 0 {
		t.Fatalf("expected expiry in the future, got %v", expiry)
	}
}

func TestClientCertExpiryFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.pem")

	if _, err := ClientCertExpiry(path); err == nil {
		t.Fatalf("expected error for missing cert")
	}

	emptyPath := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(emptyPath, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("write invalid data: %v", err)
	}
	if _, err := ClientCertExpiry(emptyPath); err == nil {
		t.Fatalf("expected error for invalid certificate data")
	}
}
