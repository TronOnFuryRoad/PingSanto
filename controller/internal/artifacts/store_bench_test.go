package artifacts

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"
)

func BenchmarkFileStoreSaveLarge(b *testing.B) {
	dir := b.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		b.Fatalf("NewFileStore: %v", err)
	}

	const size = 32 * 1024 * 1024 // 32 MiB per iteration
	b.SetBytes(size)

	for i := 0; i < b.N; i++ {
		reader := &zeroReader{remaining: size}
		meta, err := store.Save(context.Background(), SaveRequest{
			Version:      fmt.Sprintf("bench-%d", i),
			Artifact:     reader,
			ArtifactName: "bench.tar.gz",
		})
		if err != nil {
			b.Fatalf("Save iteration %d: %v", i, err)
		}
		if err := os.Remove(meta.Path); err != nil {
			b.Fatalf("remove artifact: %v", err)
		}
		if meta.SignaturePath != "" {
			_ = os.Remove(meta.SignaturePath)
		}
	}
}

type zeroReader struct {
	remaining int64
}

func (z *zeroReader) Read(p []byte) (int, error) {
	if z.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > z.remaining {
		p = p[:int(z.remaining)]
	}
	n := len(p)
	for i := 0; i < n; i++ {
		p[i] = 0
	}
	z.remaining -= int64(n)
	if z.remaining == 0 {
		return n, io.EOF
	}
	return n, nil
}
