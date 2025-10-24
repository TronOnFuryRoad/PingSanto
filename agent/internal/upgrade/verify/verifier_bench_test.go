package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkMinisignVerifierVerify(b *testing.B) {
	pubKeyPath := filepath.Clean("testdata/test.pub")
	artifactPath := filepath.Clean("testdata/artifact.bin")
	signaturePath := filepath.Clean("testdata/artifact.bin.minisig")

	pubKeyBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		b.Fatalf("read public key: %v", err)
	}
	verifier, err := NewMinisignVerifier(string(pubKeyBytes))
	if err != nil {
		b.Fatalf("NewMinisignVerifier: %v", err)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := verifier.Verify(ctx, artifactPath, signaturePath); err != nil {
			b.Fatalf("Verify failed: %v", err)
		}
	}
}
