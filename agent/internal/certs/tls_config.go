package certs

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
)

// LoadClientTLSConfig loads an mTLS client configuration using the provided
// certificate, key, and optional CA bundle. The serverURL is used to derive the
// expected ServerName for TLS verification.
func LoadClientTLSConfig(certPath, keyPath, caPath, serverURL string) (*tls.Config, error) {
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("client certificate and key paths must be provided")
	}
	if serverURL == "" {
		return nil, fmt.Errorf("server URL must be provided")
	}

	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}

	var roots *x509.CertPool
	if caPath != "" {
		data, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("invalid CA bundle")
		}
	}

	parsed, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("server URL missing hostname")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
		RootCAs:      roots,
		ServerName:   parsed.Hostname(),
	}
	return tlsConfig, nil
}
