package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	minisign "github.com/jedisct1/go-minisign"
)

// MinisignVerifier verifies artifacts signed with Minisign using a trusted public key.
type MinisignVerifier struct {
	publicKey minisign.PublicKey
}

// NewMinisignVerifier parses the provided Minisign public key (including comment header) and
// returns a verifier configured to validate signatures created with the associated secret key.
func NewMinisignVerifier(pubKey string) (*MinisignVerifier, error) {
	pubKey = strings.TrimSpace(pubKey)
	if pubKey == "" {
		return nil, errors.New("minisign public key is required")
	}
	publicKey, err := minisign.DecodePublicKey(pubKey)
	if err != nil {
		return nil, fmt.Errorf("parse minisign public key: %w", err)
	}
	return &MinisignVerifier{publicKey: publicKey}, nil
}

// Verify reads the artifact and detached signature from disk and validates the signature contents.
func (v *MinisignVerifier) Verify(ctx context.Context, artifactPath, signaturePath string) error {
	if v == nil {
		return errors.New("signature verifier not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(artifactPath) == "" {
		return errors.New("artifact path is required")
	}
	if strings.TrimSpace(signaturePath) == "" {
		return errors.New("signature path is required")
	}

	signatureBytes, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read signature %q: %w", signaturePath, err)
	}
	signature, err := minisign.DecodeSignature(string(signatureBytes))
	if err != nil {
		return fmt.Errorf("decode signature %q: %w", signaturePath, err)
	}
	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read artifact %q: %w", artifactPath, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ok, err := v.publicKey.Verify(artifactBytes, signature)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("signature verification failed")
	}
	return nil
}
