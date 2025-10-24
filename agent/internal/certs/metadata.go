package certs

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// ClientCertExpiry reads the certificate at the provided path and returns its NotAfter timestamp.
func ClientCertExpiry(certPath string) (time.Time, error) {
	if certPath == "" {
		return time.Time{}, fmt.Errorf("certificate path is empty")
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		return time.Time{}, fmt.Errorf("read certificate: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return time.Time{}, fmt.Errorf("decode certificate: no PEM block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse certificate: %w", err)
	}
	return cert.NotAfter, nil
}
