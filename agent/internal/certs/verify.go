package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

func VerifyConnection(ctx context.Context, serverURL string, certPEM, keyPEM, caPEM []byte) error {
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return fmt.Errorf("client certificate or key missing")
	}

	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("parse server url: %w", err)
	}

	if parsedURL.Scheme != "https" {
		return fmt.Errorf("unsupported server scheme %q", parsedURL.Scheme)
	}

	peer := parsedURL.Host
	if !strings.Contains(peer, ":") {
		peer = net.JoinHostPort(parsedURL.Host, "443")
	}

	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("load client certificate: %w", err)
	}

	var roots *x509.CertPool
	if len(caPEM) > 0 {
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return fmt.Errorf("invalid CA bundle")
		}
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{certificate},
		MinVersion:         tls.VersionTLS12,
		RootCAs:            roots,
		InsecureSkipVerify: false,
	}

	if host := parsedURL.Hostname(); host != "" {
		tlsConfig.ServerName = host
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}

	conn, err := tls.DialWithDialer(dialer, "tcp", peer, tlsConfig)
	if err != nil {
		return fmt.Errorf("tls dial %s: %w", peer, err)
	}
	defer conn.Close()

	if err := conn.Handshake(); err != nil {
		return fmt.Errorf("tls handshake failed: %w", err)
	}

	state := conn.ConnectionState()
	if !state.HandshakeComplete {
		return fmt.Errorf("handshake incomplete")
	}

	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("no peer certificates received")
	}

	return nil
}
